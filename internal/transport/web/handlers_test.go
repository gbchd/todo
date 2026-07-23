package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func newTestMux(t *testing.T) http.Handler {
	t.Helper()
	repo, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "todo.db"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return NewMux(todo.NewService(repo))
}

func doJSON(t *testing.T, mux http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeTask(t *testing.T, rec *httptest.ResponseRecorder) taskDTO {
	t.Helper()
	var task taskDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode task: %v (body=%s)", err, rec.Body.String())
	}
	return task
}

func TestListTasks_Empty(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "GET", "/api/tasks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %q, want []", rec.Body.String())
	}
}

func TestCreateAndListTasks(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "Buy milk", Priority: "high"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	created := decodeTask(t, rec)
	if created.Title != "Buy milk" || created.Priority != "high" || created.Status != "open" {
		t.Errorf("created = %+v", created)
	}

	rec = doJSON(t, mux, "GET", "/api/tasks", nil)
	var tasks []taskDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != created.ID {
		t.Errorf("tasks = %+v", tasks)
	}
}

func TestCreateTask_WithDueDate(t *testing.T) {
	mux := newTestMux(t)
	due := "2026-08-01"
	rec := doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x", DueDate: &due})
	created := decodeTask(t, rec)
	if created.DueDate == nil || *created.DueDate != due {
		t.Errorf("DueDate = %v, want %s", created.DueDate, due)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "GET", "/api/tasks/999", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPatchTask_PartialUpdate(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "orig", Description: "desc"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"priority": "high"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	updated := decodeTask(t, rec)
	if updated.Priority != "high" {
		t.Errorf("Priority = %q, want high", updated.Priority)
	}
	if updated.Title != "orig" || updated.Description != "desc" {
		t.Errorf("unrelated fields changed: %+v", updated)
	}
}

func TestPatchTask_ClearDueDateWithNull(t *testing.T) {
	mux := newTestMux(t)
	due := "2026-08-01"
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x", DueDate: &due}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"due_date": nil})
	updated := decodeTask(t, rec)
	if updated.DueDate != nil {
		t.Errorf("DueDate = %v, want nil after clearing", updated.DueDate)
	}
}

func TestPatchTask_StatusDrivesVerb(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"status": "done"})
	updated := decodeTask(t, rec)
	if updated.Status != "done" {
		t.Fatalf("Status = %q, want done", updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Errorf("CompletedAt not set after status=done")
	}
}

func TestPatchTask_FieldsAndStatusTogether(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"title": "renamed", "status": "in-progress"})
	updated := decodeTask(t, rec)
	if updated.Title != "renamed" || updated.Status != "in-progress" {
		t.Errorf("updated = %+v", updated)
	}
}

func TestPatchTask_InvalidPriority(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"priority": "urgent"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDeleteTask(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "DELETE", "/api/tasks/"+itoa(created.ID), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	rec = doJSON(t, mux, "GET", "/api/tasks/"+itoa(created.ID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 after delete", rec.Code)
	}
}

func TestStaticIndexServed(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "GET", "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<title>todo</title>") {
		t.Errorf("index.html body missing expected title tag")
	}

	// The built SPA's asset filenames are content-hashed by Vite, so
	// extract the referenced bundle path from index.html rather than
	// assuming a fixed name like the old hand-written app.js.
	m := regexp.MustCompile(`src="(/assets/[^"]+\.js)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("index.html has no referenced JS bundle: %s", body)
	}
	rec = doJSON(t, mux, "GET", m[1], nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bundle %s status = %d", m[1], rec.Code)
	}
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
