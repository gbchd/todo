// Package web implements the `todo serve` localhost JSON API + vanilla-JS
// SPA adapter over the shared core Service.
package web

import (
	"context"
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

func listTasks(svc *todo.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tasks, err := svc.ListTasks(r.Context(), todo.TaskFilter{})
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
			d, err := time.Parse("2006-01-02", *req.DueDate)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid due_date"})
				return
			}
			input.DueDate = &d
		}

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
// title/description/priority/due_date/status may be present; absent keys
// are left untouched. due_date's JSON null explicitly clears it, mirroring
// the tri-state semantics of todo.TaskPatch.
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

		patch, statusChange, err := parsePatch(body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		t, err := svc.GetTask(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}

		if hasPatch(patch) {
			t, err = svc.UpdateTask(r.Context(), id, patch)
			if err != nil {
				writeError(w, err)
				return
			}
		}
		if statusChange != nil {
			t, err = applyStatus(r.Context(), svc, id, *statusChange)
			if err != nil {
				writeError(w, err)
				return
			}
		}

		writeJSON(w, http.StatusOK, toDTO(t))
	}
}

func hasPatch(p todo.TaskPatch) bool {
	return p.Title.IsSet() || p.Description.IsSet() || p.Priority.IsSet() || p.DueDate.IsSet()
}

func parsePatch(body map[string]json.RawMessage) (todo.TaskPatch, *todo.Status, error) {
	patch := todo.TaskPatch{}

	if raw, ok := body["title"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return patch, nil, errors.New("invalid title")
		}
		patch.Title = todo.Set(v)
	}
	if raw, ok := body["description"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return patch, nil, errors.New("invalid description")
		}
		patch.Description = todo.Set(v)
	}
	if raw, ok := body["priority"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return patch, nil, errors.New("invalid priority")
		}
		patch.Priority = todo.Set(todo.Priority(v))
	}
	if raw, ok := body["due_date"]; ok {
		var v *string
		if err := json.Unmarshal(raw, &v); err != nil {
			return patch, nil, errors.New("invalid due_date")
		}
		if v == nil {
			patch.DueDate = todo.Set[*time.Time](nil)
		} else {
			d, err := time.Parse("2006-01-02", *v)
			if err != nil {
				return patch, nil, errors.New("invalid due_date")
			}
			patch.DueDate = todo.Set(&d)
		}
	}

	var statusChange *todo.Status
	if raw, ok := body["status"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return patch, nil, errors.New("invalid status")
		}
		s := todo.Status(v)
		statusChange = &s
	}

	return patch, statusChange, nil
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

// applyStatus maps a target lifecycle status onto the corresponding
// Service verb (Start/Complete/Reopen), the same convention the TUI adapter
// uses, so no invalid status string is ever constructed.
func applyStatus(ctx context.Context, svc *todo.Service, id int64, target todo.Status) (todo.Task, error) {
	switch target {
	case todo.StatusOpen:
		return svc.ReopenTask(ctx, id)
	case todo.StatusInProgress:
		return svc.StartTask(ctx, id)
	case todo.StatusDone:
		return svc.CompleteTask(ctx, id)
	default:
		return todo.Task{}, &todo.ValidationError{Field: "status", Message: "invalid status " + string(target)}
	}
}
