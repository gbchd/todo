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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func newTestMux(t *testing.T) http.Handler {
	t.Helper()
	repo, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "todo.db"))
	require.NoError(t, err, "open repo")
	t.Cleanup(func() { repo.Close() })
	return NewMux(todo.NewService(repo))
}

func doJSON(t *testing.T, mux http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err, "marshal body")
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, reader)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeTask(t *testing.T, rec *httptest.ResponseRecorder) taskDTO {
	t.Helper()
	var task taskDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &task), "decode task (body=%s)", rec.Body.String())
	return task
}

func TestListTasks_Empty(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "GET", "/api/tasks", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))
}

func TestCreateAndListTasks(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "Buy milk", Priority: "high"})
	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	created := decodeTask(t, rec)
	assert.Equal(t, "Buy milk", created.Title)
	assert.Equal(t, "high", created.Priority)
	assert.Equal(t, "open", created.Status)

	rec = doJSON(t, mux, "GET", "/api/tasks", nil)
	var tasks []taskDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tasks), "decode list")
	require.Len(t, tasks, 1)
	assert.Equal(t, created.ID, tasks[0].ID)
}

func TestCreateTask_WithDueDate(t *testing.T) {
	mux := newTestMux(t)
	due := "2026-08-01"
	rec := doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x", DueDate: &due})
	created := decodeTask(t, rec)
	require.NotNil(t, created.DueDate)
	assert.Equal(t, due, *created.DueDate)
}

func TestGetTask_NotFound(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "GET", "/api/tasks/999", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPatchTask_PartialUpdate(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "orig", Description: "desc"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"priority": "high"})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	updated := decodeTask(t, rec)
	assert.Equal(t, "high", updated.Priority)
	assert.Equal(t, "orig", updated.Title)
	assert.Equal(t, "desc", updated.Description)
}

func TestPatchTask_ClearDueDateWithNull(t *testing.T) {
	mux := newTestMux(t)
	due := "2026-08-01"
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x", DueDate: &due}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"due_date": nil})
	updated := decodeTask(t, rec)
	assert.Nil(t, updated.DueDate)
}

func TestPatchTask_StatusDrivesVerb(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"status": "done"})
	updated := decodeTask(t, rec)
	require.Equal(t, "done", updated.Status)
	assert.NotNil(t, updated.CompletedAt)
}

func TestPatchTask_FieldsAndStatusTogether(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"title": "renamed", "status": "in-progress"})
	updated := decodeTask(t, rec)
	assert.Equal(t, "renamed", updated.Title)
	assert.Equal(t, "in-progress", updated.Status)
}

func TestPatchTask_InvalidPriority(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"priority": "urgent"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestPatchTask_InvalidStatusLeavesFieldUnchanged is the regression for
// Finding 2: PATCH {"title":"CHANGED","status":"bogus"} used to return 400
// but still persist the title change. The whole patch must now be rejected
// atomically.
func TestPatchTask_InvalidStatusLeavesFieldUnchanged(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "orig"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"title": "CHANGED", "status": "bogus"})
	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
	assert.JSONEq(t, `{"error":"status: invalid status bogus"}`, rec.Body.String())

	rec = doJSON(t, mux, "GET", "/api/tasks/"+itoa(created.ID), nil)
	got := decodeTask(t, rec)
	assert.Equal(t, "orig", got.Title, "rejected patch must not persist the title change")
}

func TestPatchTask_ValidFieldAndValidStatusAppliesBoth(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "orig"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{"title": "renamed", "status": "done"})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	updated := decodeTask(t, rec)
	assert.Equal(t, "renamed", updated.Title)
	assert.Equal(t, "done", updated.Status)
	assert.NotNil(t, updated.CompletedAt)
}

func TestPatchTask_EmptyBodyDoesNotBumpUpdatedAt(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(created.ID), map[string]any{})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	updated := decodeTask(t, rec)
	assert.Equal(t, created.UpdatedAt, updated.UpdatedAt, "empty patch should not bump UpdatedAt")
}

func TestDeleteTask(t *testing.T) {
	mux := newTestMux(t)
	created := decodeTask(t, doJSON(t, mux, "POST", "/api/tasks", createRequest{Title: "x"}))

	rec := doJSON(t, mux, "DELETE", "/api/tasks/"+itoa(created.ID), nil)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = doJSON(t, mux, "GET", "/api/tasks/"+itoa(created.ID), nil)
	require.Equal(t, http.StatusNotFound, rec.Code, "want 404 after delete")
}

func TestStaticIndexServed(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "GET", "/", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<title>todo</title>")

	// The built SPA's asset filenames are content-hashed by Vite, so
	// extract the referenced bundle path from index.html rather than
	// assuming a fixed name like the old hand-written app.js.
	m := regexp.MustCompile(`src="(/assets/[^"]+\.js)"`).FindStringSubmatch(body)
	require.NotNil(t, m, "index.html has no referenced JS bundle: %s", body)
	rec = doJSON(t, mux, "GET", m[1], nil)
	assert.Equal(t, http.StatusOK, rec.Code, "bundle %s", m[1])
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
