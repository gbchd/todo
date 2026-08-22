// Package host implements the `todo host` adapter: the versioned HTTP task
// API that another instance of todo reads and writes through.
//
// It is deliberately separate from internal/transport/web, which serves the
// local web UI and its own API. That one is free to change shape whenever the
// bundled frontend does, because both ship in the same binary; this one is a
// contract between two binaries that may be different builds, which is why its
// path carries a version from the first commit. The host serves the task API
// and nothing else — no web UI.
package host

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gbchd/todo/internal/pairing"
	"github.com/gbchd/todo/internal/service/todo"
)

// apiPrefix versions the wire contract in the path. An older client reaching a
// newer host asks for a prefix that host may no longer route, which is a
// clearer failure than a subtly different payload under the same URL.
const apiPrefix = "/api/v1"

// Middleware wraps the task API handler, outermost first.
//
// It is the seam authentication mounts on: an auth middleware passed here
// guards every task route while leaving routes registered outside the version
// prefix — a pairing endpoint, which by definition serves devices that have no
// credential yet — reachable without one.
type Middleware func(http.Handler) http.Handler

// NewMux builds the host's HTTP handler: the versioned task API wrapped in mw,
// the pairing route outside it, and nothing else. Exported so tests can drive
// it via httptest without starting a real listener.
//
// pairs may be nil, which leaves the pairing route unmounted. It makes no
// observable difference: a mounted pairing route with no offer outstanding
// answers exactly as this mux answers for a path it does not route.
func NewMux(svc *todo.Service, pairs *pairing.Store, mw ...Middleware) http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET "+apiPrefix+"/tasks", listTasks(svc))
	api.HandleFunc("POST "+apiPrefix+"/tasks", createTask(svc))
	api.HandleFunc("GET "+apiPrefix+"/tasks/{id}", getTask(svc))
	api.HandleFunc("PATCH "+apiPrefix+"/tasks/{id}", patchTask(svc))
	api.HandleFunc("DELETE "+apiPrefix+"/tasks/{id}", deleteTask(svc))

	// Two muxes, not one: everything under the version prefix goes through mw,
	// and the outer mux is where the unauthenticated route is registered. The
	// outer mux has no "/" handler, so every other path 404s.
	mux := http.NewServeMux()
	mux.Handle(apiPrefix+"/", wrap(api, mw))
	if pairs != nil {
		// Registered without a method, so that the wrong method reaches the
		// handler and is answered with a 404 too. A method-scoped pattern
		// would have the mux itself reply 405, which announces that the route
		// exists to anyone who sends a GET.
		mux.HandleFunc(pairing.Path, pairDevice(pairs, newRateLimiter(pairBurst, pairRefill)))
	}
	return mux
}

func wrap(h http.Handler, mw []Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, errorBody{Error: msg})
}

// writeError maps the domain's error vocabulary onto status codes: ErrNotFound
// to 404, a rejected field to 400, and a stale version to 409. Each carries the
// structured fields a client needs to rebuild the same error on its side.
func writeError(w http.ResponseWriter, err error) {
	var cerr *todo.ConflictError
	if errors.As(err, &cerr) {
		writeJSON(w, http.StatusConflict, errorBody{
			Error:    cerr.Error(),
			Conflict: &conflictBody{TaskID: cerr.TaskID, Expected: cerr.Expected, Actual: cerr.Actual},
		})
		return
	}
	var verr *todo.ValidationError
	if errors.As(err, &verr) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: verr.Error(), Field: verr.Field, Message: verr.Message})
		return
	}
	status := http.StatusInternalServerError
	if errors.Is(err, todo.ErrNotFound) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, errorBody{Error: err.Error()})
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// listTasks serves the whole TaskFilter vocabulary. It goes through the
// Service like every other verb, so the sort and the Subtask grouping a local
// caller gets are the same ones a remote caller gets.
func listTasks(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, err := parseFilter(r.URL.Query())
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		tasks, err := svc.ListTasks(r.Context(), filter)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toDTOs(tasks))
	}
}

// parseFilter reads a TaskFilter off the query string.
//
// parent is three-valued and so is read by key presence, never by value: an
// absent parent key leaves the listing unconstrained, parent=none restricts it
// to top-level tasks, and parent=<id> restricts it to that task's Subtasks.
// Reading it with Query().Get would collapse the first two states into one,
// because an absent key and an empty value are the same empty string.
//
// status, priority and sort are passed through as raw strings: the Service
// validates them and its *todo.ValidationError names the offending field, so
// re-checking them here would only duplicate the vocabulary.
func parseFilter(q url.Values) (todo.TaskFilter, error) {
	filter := todo.TaskFilter{SortBy: todo.SortKey(q.Get("sort"))}

	if q.Has("status") {
		s := todo.Status(q.Get("status"))
		filter.Status = &s
	}
	if q.Has("priority") {
		p := todo.Priority(q.Get("priority"))
		filter.Priority = &p
	}

	var err error
	if filter.DueBefore, err = parseDateParam(q, "due_before"); err != nil {
		return filter, err
	}
	if filter.DueAfter, err = parseDateParam(q, "due_after"); err != nil {
		return filter, err
	}

	if q.Has("parent") {
		raw := q.Get("parent")
		if raw == "none" {
			filter.ParentID = todo.Set[*int64](nil)
		} else {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return filter, errors.New("invalid parent")
			}
			filter.ParentID = todo.Set(&id)
		}
	}

	return filter, nil
}

func parseDateParam(q url.Values, key string) (*time.Time, error) {
	if !q.Has(key) {
		return nil, nil
	}
	d, err := time.Parse(todo.DateLayout, q.Get(key))
	if err != nil {
		return nil, errors.New("invalid " + key)
	}
	return &d, nil
}

// createTask creates the task in the state the request asked for, in one
// request, because the client's repository adapter must not have to follow a
// create it cannot retry with a second write that can fail on its own.
//
// It still goes through the Service twice — AddTask, then UpdateTask for a
// status other than the one a task opens in — because the host has no other
// way in and the lifecycle rules that stamp CompletedAt live there. Two calls
// are safe here where two HTTP requests are not: nothing is written to the
// response between them, so the client sees either the created task or an
// error, never a task it was told did not exist.
func createTask(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badRequest(w, "invalid JSON body")
			return
		}

		input := todo.NewTask{
			Title:       req.Title,
			Description: req.Description,
			Priority:    todo.Priority(req.Priority),
			ParentID:    req.ParentID,
		}
		if req.DueDate != nil {
			d, err := time.Parse(todo.DateLayout, *req.DueDate)
			if err != nil {
				badRequest(w, "invalid due_date")
				return
			}
			input.DueDate = &d
		}

		t, err := svc.AddTask(r.Context(), input)
		if err != nil {
			writeError(w, err)
			return
		}
		if t, err = openIn(r, svc, t, todo.Status(req.Status)); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toDTO(t))
	}
}

// openIn moves a freshly created task to the status the request asked for,
// and is a no-op for the status the Service already opened it in.
//
// A rejected status undoes the create rather than leaving a task nobody asked
// for behind: the request answers with an error, so the task it half-made must
// not survive it. The delete cannot be observed as a separate step — no reply
// has been written yet — which is exactly what makes this compensation sound
// here and unsound across a network.
func openIn(r *http.Request, svc *todo.Service, t todo.Task, status todo.Status) (todo.Task, error) {
	if status == "" || status == t.Status {
		return t, nil
	}
	updated, err := svc.UpdateTask(r.Context(), t.ID, todo.TaskPatch{Status: todo.Set(status)})
	if err != nil {
		svc.DeleteTask(r.Context(), t.ID) //nolint:errcheck // the request is failing either way; a stranded task is the worse of the two
		return todo.Task{}, err
	}
	return updated, nil
}

func getTask(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			badRequest(w, "invalid task id")
			return
		}
		t, err := svc.GetTask(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toDTO(t))
	}
}

// patchTask applies a partial patch. Any subset of
// title/description/priority/due_date/parent_id/status may be present; absent
// keys are left untouched, a JSON null clears due_date and promotes a Subtask
// back to top level for parent_id. An expected_version makes the whole patch
// conditional on nobody having written since; absent, the patch is
// unconditional, which is what a local caller has always done.
func patchTask(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			badRequest(w, "invalid task id")
			return
		}

		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid JSON body")
			return
		}

		patch, err := parsePatch(body)
		if err != nil {
			badRequest(w, err.Error())
			return
		}

		t, err := svc.UpdateTask(r.Context(), id, patch)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toDTO(t))
	}
}

// readOnlyFields are the parts of taskDTO storage owns: the identity and
// timestamps the Service stamps, the version it increments, and the Subtask
// rollup the repository derives. A PATCH naming one is rejected rather than
// ignored — silently dropping child_count would let a client believe it had
// written a number that storage will contradict on the next read.
var readOnlyFields = []string{
	"id", "created_at", "updated_at", "completed_at",
	"version", "child_count", "done_child_count", "any_child_overdue",
}

// setPatchField unmarshals body[key] into an Optional[T] field when key is
// present, leaving dst untouched otherwise. Optional's "was it provided"
// question is exactly JSON object key presence, which is why the body is
// decoded into raw messages rather than a struct.
func setPatchField[T any](body map[string]json.RawMessage, key string, dst *todo.Optional[T]) error {
	raw, ok := body[key]
	if !ok {
		return nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return errors.New("invalid " + key)
	}
	*dst = todo.Set(v)
	return nil
}

func parsePatch(body map[string]json.RawMessage) (todo.TaskPatch, error) {
	patch := todo.TaskPatch{}

	for _, field := range readOnlyFields {
		if _, ok := body[field]; ok {
			return patch, errors.New(field + " is read-only")
		}
	}

	if err := setPatchField(body, "title", &patch.Title); err != nil {
		return patch, err
	}
	if err := setPatchField(body, "description", &patch.Description); err != nil {
		return patch, err
	}
	if err := setPatchField(body, "priority", &patch.Priority); err != nil {
		return patch, err
	}
	if err := setPatchField(body, "parent_id", &patch.ParentID); err != nil {
		return patch, err
	}
	if err := setPatchField(body, "status", &patch.Status); err != nil {
		return patch, err
	}
	if err := setPatchField(body, "expected_version", &patch.ExpectedVersion); err != nil {
		return patch, err
	}

	if raw, ok := body["due_date"]; ok {
		var v *string
		if err := json.Unmarshal(raw, &v); err != nil {
			return patch, errors.New("invalid due_date")
		}
		if v == nil {
			patch.DueDate = todo.Set[*time.Time](nil)
		} else {
			d, err := time.Parse(todo.DateLayout, *v)
			if err != nil {
				return patch, errors.New("invalid due_date")
			}
			patch.DueDate = todo.Set(&d)
		}
	}

	return patch, nil
}

func deleteTask(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			badRequest(w, "invalid task id")
			return
		}
		if err := svc.DeleteTask(r.Context(), id); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
