// Package web implements the `todo serve` localhost JSON API + vanilla-JS
// SPA adapter over the shared core Service.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/gbchd/todo/internal/service/todo"
)

//go:embed static
var staticFS embed.FS

// staticContent strips the "static/" embed prefix so index.html serves at
// "/" rather than "/static/".
var staticContent = must(fs.Sub(staticFS, "static"))

func must(f fs.FS, err error) fs.FS {
	if err != nil {
		panic(err) // embedded at build time; cannot fail at runtime
	}
	return f
}

// NewMux builds the HTTP handler: the JSON API under /api/tasks plus the
// embedded static SPA. Exported so tests can drive it via httptest without
// starting a real listener.
func NewMux(svc *todo.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tasks", listTasks(svc))
	mux.HandleFunc("POST /api/tasks", createTask(svc))
	mux.HandleFunc("GET /api/tasks/{id}", getTask(svc))
	mux.HandleFunc("PATCH /api/tasks/{id}", patchTask(svc))
	mux.HandleFunc("DELETE /api/tasks/{id}", deleteTask(svc))
	mux.Handle("/", http.FileServerFS(staticContent))
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, todo.ErrNotFound) {
		status = http.StatusNotFound
	}
	var verr *todo.ValidationError
	if errors.As(err, &verr) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// listTasks reads the tri-state ?parent= query parameter: absent leaves the
// listing unconstrained (what every caller got before subtasks existed),
// "none" restricts it to top-level tasks, and an id restricts it to that
// task's Subtasks.
func listTasks(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := todo.TaskFilter{}
		if raw := r.URL.Query().Get("parent"); raw != "" {
			if raw == "none" {
				filter.ParentID = todo.Set[*int64](nil)
			} else {
				id, err := strconv.ParseInt(raw, 10, 64)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid parent"})
					return
				}
				filter.ParentID = todo.Set(&id)
			}
		}

		tasks, err := svc.ListTasks(r.Context(), filter)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toDTOs(tasks))
	}
}

func createTask(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}

		input := todo.NewTask{
			Title:       req.Title,
			Description: req.Description,
			Priority:    todo.Priority(req.Priority),
		}
		if req.DueDate != nil {
			d, err := time.Parse(todo.DateLayout, *req.DueDate)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid due_date"})
				return
			}
			input.DueDate = &d
		}
		input.ParentID = req.ParentID

		t, err := svc.AddTask(r.Context(), input)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toDTO(t))
	}
}

func getTask(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
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
// keys are left untouched. A JSON null clears due_date, and promotes a Subtask
// back to top level for parent_id — the key-presence check is exactly the
// tri-state todo.Optional expects, so no extra machinery is needed. The whole
// patch, status included, is applied by one Service.UpdateTask call so a
// rejected field (e.g. an invalid status) can never leave a prior field
// half-applied.
func patchTask(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
			return
		}

		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}

		patch, err := parsePatch(body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

// setPatchField unmarshals body[key] into an Optional[T] field when key is
// present, leaving dst untouched otherwise. Covers the mechanical patch
// fields whose wire and domain shapes match one-for-one; due_date (needs a
// date parse) and status (validated inside the domain, not here) stay
// bespoke in parsePatch.
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

	if raw, ok := body["status"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return patch, errors.New("invalid status")
		}
		patch.Status = todo.Set(todo.Status(v))
	}

	return patch, nil
}

func deleteTask(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
			return
		}
		if err := svc.DeleteTask(r.Context(), id); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
