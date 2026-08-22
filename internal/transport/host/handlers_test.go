package host

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func newTestMux(t *testing.T, mw ...Middleware) http.Handler {
	t.Helper()
	return NewMux(newTestService(t, filepath.Join(t.TempDir(), "todo.db")), nil, mw...)
}

func newTestService(t *testing.T, dbPath string) *todo.Service {
	t.Helper()
	repo, err := repository.Open(context.Background(), dbPath)
	require.NoError(t, err, "open repo")
	t.Cleanup(func() { repo.Close() })
	return todo.NewService(repo)
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

func decodeTasks(t *testing.T, rec *httptest.ResponseRecorder) []taskDTO {
	t.Helper()
	var tasks []taskDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tasks), "decode tasks (body=%s)", rec.Body.String())
	return tasks
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "decode error (body=%s)", rec.Body.String())
	return body
}

func createTaskViaAPI(t *testing.T, mux http.Handler, body map[string]any) taskDTO {
	t.Helper()
	rec := doJSON(t, mux, http.MethodPost, "/api/v1/tasks", body)
	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	return decodeTask(t, rec)
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func taskPath(id int64) string { return "/api/v1/tasks/" + itoa(id) }

func TestListTasks_Empty(t *testing.T) {
	rec := doJSON(t, newTestMux(t), http.MethodGet, "/api/v1/tasks", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, rec.Body.String())
}

func TestCreateAndListTasks(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk", "priority": "high", "due_date": "2026-08-01"})
	assert.Equal(t, "Buy milk", created.Title)
	assert.Equal(t, "open", created.Status)
	require.NotNil(t, created.DueDate)
	assert.Equal(t, "2026-08-01", *created.DueDate)
	assert.Equal(t, int64(1), created.Version, "a freshly created task is at version 1")

	rec := doJSON(t, mux, http.MethodGet, "/api/v1/tasks", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	tasks := decodeTasks(t, rec)
	require.Len(t, tasks, 1)
	assert.Equal(t, created.ID, tasks[0].ID)
}

func TestGetTask(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Read a book"})

	rec := doJSON(t, mux, http.MethodGet, taskPath(created.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "Read a book", decodeTask(t, rec).Title)
}

func TestGetTask_NotFound(t *testing.T) {
	rec := doJSON(t, newTestMux(t), http.MethodGet, "/api/v1/tasks/999", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, todo.ErrNotFound.Error(), decodeError(t, rec).Error)
}

func TestCreateTask_ValidationErrorCarriesField(t *testing.T) {
	rec := doJSON(t, newTestMux(t), http.MethodPost, "/api/v1/tasks", map[string]any{"title": "  "})
	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())

	body := decodeError(t, rec)
	assert.Equal(t, "title", body.Field, "a client must rebuild the ValidationError from fields, not from prose")
	assert.Equal(t, "must not be empty", body.Message)
	assert.Equal(t, "title: must not be empty", body.Error)
}

// A client's repository adapter is handed whole tasks and cannot follow a
// create it may not retry with a second write, so the status it was given goes
// out in the create itself — and the lifecycle rule that stamps CompletedAt
// still applies, because the host reaches it through the Service.
func TestCreateTask_CreatesTheTaskInTheStatusAsked(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk", "status": "done"})

	assert.Equal(t, "done", created.Status)
	assert.NotNil(t, created.CompletedAt, "a task created done is completed at the host's clock")

	// And it is that task on the host, not just in the answer.
	stored := decodeTask(t, doJSON(t, mux, http.MethodGet, taskPath(created.ID), nil))
	assert.Equal(t, "done", stored.Status)
	assert.NotNil(t, stored.CompletedAt)
}

func TestCreateTask_OpensATaskThatNamesNoStatus(t *testing.T) {
	created := createTaskViaAPI(t, newTestMux(t), map[string]any{"title": "Buy milk"})
	assert.Equal(t, "open", created.Status)
	assert.Nil(t, created.CompletedAt)
}

// The request failed, so the task it half-made must not outlive it: the client
// is told nothing was created, and nothing was.
func TestCreateTask_LeavesNothingBehindWhenTheStatusIsRejected(t *testing.T) {
	mux := newTestMux(t)

	rec := doJSON(t, mux, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "Buy milk", "status": "nonsense"})
	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "status", decodeError(t, rec).Field)

	listed := decodeTasks(t, doJSON(t, mux, http.MethodGet, "/api/v1/tasks", nil))
	assert.Empty(t, listed, "a rejected create must not leave a task behind")
}

// The write model is narrower than the read model on purpose: storage owns the
// version and the rollup, so a create that names them must not be able to set
// them.
func TestCreateTask_IgnoresRepositoryOwnedFields(t *testing.T) {
	created := createTaskViaAPI(t, newTestMux(t), map[string]any{
		"title": "Buy milk", "version": 42, "child_count": 7, "done_child_count": 3, "any_child_overdue": true,
	})
	assert.Equal(t, int64(1), created.Version)
	assert.Equal(t, 0, created.ChildCount)
	assert.Equal(t, 0, created.DoneChildCount)
	assert.False(t, created.AnyChildOverdue)
}

func TestPatchTask_PartialUpdate(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk", "description": "from the corner shop"})

	rec := doJSON(t, mux, http.MethodPatch, taskPath(created.ID), map[string]any{"status": "done"})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	patched := decodeTask(t, rec)
	assert.Equal(t, "done", patched.Status)
	assert.NotNil(t, patched.CompletedAt)
	assert.Equal(t, "from the corner shop", patched.Description, "an absent key leaves the field untouched")
	assert.Greater(t, patched.Version, created.Version, "every write bumps the version")
}

func TestPatchTask_ClearDueDateWithNull(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk", "due_date": "2026-08-01"})

	rec := doJSON(t, mux, http.MethodPatch, taskPath(created.ID), map[string]any{"due_date": nil})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Nil(t, decodeTask(t, rec).DueDate)
}

func TestPatchTask_InvalidStatus(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk"})

	rec := doJSON(t, mux, http.MethodPatch, taskPath(created.ID), map[string]any{"status": "bogus"})
	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "status", decodeError(t, rec).Field)
}

func TestPatchTask_NotFound(t *testing.T) {
	rec := doJSON(t, newTestMux(t), http.MethodPatch, "/api/v1/tasks/999", map[string]any{"title": "Nope"})
	require.Equal(t, http.StatusNotFound, rec.Code, "body=%s", rec.Body.String())
}

func TestPatchTask_ExpectedVersionMatches(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk"})

	rec := doJSON(t, mux, http.MethodPatch, taskPath(created.ID),
		map[string]any{"title": "Buy oat milk", "expected_version": created.Version})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "Buy oat milk", decodeTask(t, rec).Title)
}

// The 409 body carries the three numbers a client needs to rebuild the
// *todo.ConflictError its caller will see, so no adapter has to parse the
// message to find them.
func TestPatchTask_ExpectedVersionMismatchConflicts(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk"})

	stale := created.Version
	bump := doJSON(t, mux, http.MethodPatch, taskPath(created.ID), map[string]any{"title": "Buy oat milk"})
	require.Equal(t, http.StatusOK, bump.Code, "body=%s", bump.Body.String())
	current := decodeTask(t, bump).Version

	rec := doJSON(t, mux, http.MethodPatch, taskPath(created.ID),
		map[string]any{"title": "Buy soy milk", "expected_version": stale})
	require.Equal(t, http.StatusConflict, rec.Code, "body=%s", rec.Body.String())

	body := decodeError(t, rec)
	require.NotNil(t, body.Conflict)
	assert.Equal(t, created.ID, body.Conflict.TaskID)
	assert.Equal(t, stale, body.Conflict.Expected)
	assert.Equal(t, current, body.Conflict.Actual)

	after := doJSON(t, mux, http.MethodGet, taskPath(created.ID), nil)
	assert.Equal(t, "Buy oat milk", decodeTask(t, after).Title, "a rejected patch leaves the task untouched")
}

// Absent expected_version must stay unconditional: that is what every local
// caller sends, and making a bare patch conditional would break them.
func TestPatchTask_AbsentExpectedVersionIsUnconditional(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk"})
	require.Equal(t, http.StatusOK,
		doJSON(t, mux, http.MethodPatch, taskPath(created.ID), map[string]any{"title": "one"}).Code)

	rec := doJSON(t, mux, http.MethodPatch, taskPath(created.ID), map[string]any{"title": "two"})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "two", decodeTask(t, rec).Title)
}

func TestPatchTask_RejectsRepositoryOwnedFields(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk"})

	for _, field := range readOnlyFields {
		t.Run(field, func(t *testing.T) {
			rec := doJSON(t, mux, http.MethodPatch, taskPath(created.ID), map[string]any{field: 1})
			require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
			assert.Equal(t, field+" is read-only", decodeError(t, rec).Error)
		})
	}
}

func TestDeleteTask(t *testing.T) {
	mux := newTestMux(t)
	created := createTaskViaAPI(t, mux, map[string]any{"title": "Buy milk"})

	rec := doJSON(t, mux, http.MethodDelete, taskPath(created.ID), nil)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, http.StatusNotFound, doJSON(t, mux, http.MethodGet, taskPath(created.ID), nil).Code)
}

func TestDeleteTask_NotFound(t *testing.T) {
	rec := doJSON(t, newTestMux(t), http.MethodDelete, "/api/v1/tasks/999", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, "body=%s", rec.Body.String())
}

func TestListTasks_Filters(t *testing.T) {
	mux := newTestMux(t)
	createTaskViaAPI(t, mux, map[string]any{"title": "milk", "priority": "high", "due_date": "2026-08-01"})
	bread := createTaskViaAPI(t, mux, map[string]any{"title": "bread", "priority": "low", "due_date": "2026-09-01"})
	require.Equal(t, http.StatusOK,
		doJSON(t, mux, http.MethodPatch, taskPath(bread.ID), map[string]any{"status": "done"}).Code)

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"status", "?status=done", []string{"bread"}},
		{"priority", "?priority=high", []string{"milk"}},
		{"due_before", "?due_before=2026-08-15", []string{"milk"}},
		{"due_after", "?due_after=2026-08-15", []string{"bread"}},
		{"sort by id", "?sort=id", []string{"milk", "bread"}},
		{"sort by priority", "?sort=priority", []string{"milk", "bread"}},
		{"unfiltered", "", []string{"milk", "bread"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(t, mux, http.MethodGet, "/api/v1/tasks"+tt.query, nil)
			require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
			titles := make([]string, 0, len(tt.want))
			for _, task := range decodeTasks(t, rec) {
				titles = append(titles, task.Title)
			}
			assert.Equal(t, tt.want, titles)
		})
	}
}

// The tri-state parent filter is the encoding most likely to go wrong on the
// wire: unset, explicitly top-level, and a specific Parent Task have to stay
// three distinguishable states, which is why presence is read from the key and
// not from the value.
func TestListTasks_ParentTriState(t *testing.T) {
	mux := newTestMux(t)
	parent := createTaskViaAPI(t, mux, map[string]any{"title": "parent"})
	child := createTaskViaAPI(t, mux, map[string]any{"title": "child", "parent_id": parent.ID})
	other := createTaskViaAPI(t, mux, map[string]any{"title": "other"})

	tests := []struct {
		name  string
		query string
		want  []int64
	}{
		{"unset matches every task", "/api/v1/tasks", []int64{parent.ID, child.ID, other.ID}},
		{"none matches top-level only", "/api/v1/tasks?parent=none", []int64{parent.ID, other.ID}},
		{"an id matches that parent's subtasks", "/api/v1/tasks?parent=" + itoa(parent.ID), []int64{child.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(t, mux, http.MethodGet, tt.query, nil)
			require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
			ids := make([]int64, 0, len(tt.want))
			for _, task := range decodeTasks(t, rec) {
				ids = append(ids, task.ID)
			}
			assert.ElementsMatch(t, tt.want, ids)
		})
	}
}

func TestListTasks_RollupIsPopulatedOnRead(t *testing.T) {
	mux := newTestMux(t)
	parent := createTaskViaAPI(t, mux, map[string]any{"title": "parent"})
	createTaskViaAPI(t, mux, map[string]any{"title": "child", "parent_id": parent.ID})

	rec := doJSON(t, mux, http.MethodGet, taskPath(parent.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, 1, decodeTask(t, rec).ChildCount)
}

func TestListTasks_BadParameters(t *testing.T) {
	mux := newTestMux(t)
	tests := []struct{ name, query string }{
		{"parent", "?parent=abc"},
		{"due_before", "?due_before=nonsense"},
		{"due_after", "?due_after=nonsense"},
		{"status", "?status=bogus"},
		{"priority", "?priority=bogus"},
		{"sort", "?sort=bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(t, mux, http.MethodGet, "/api/v1/tasks"+tt.query, nil)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
		})
	}
}

// The host serves the task API and nothing else. Anything outside the version
// prefix — the web UI's own API path included — is simply not routed.
func TestNoWebUIIsServed(t *testing.T) {
	mux := newTestMux(t)
	for _, path := range []string{"/", "/index.html", "/api/tasks", "/api/v2/tasks"} {
		rec := doJSON(t, mux, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, "path %s must not be served", path)
	}
}

// Middleware is the seam authentication mounts on: it has to see every task
// route, and it has to be able to answer before the handler runs.
func TestMiddlewareWrapsEveryTaskRoute(t *testing.T) {
	var seen []string
	mux := newTestMux(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, r.Method+" "+r.URL.Path)
			if r.Header.Get("X-Test-Reject") != "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	require.Equal(t, http.StatusOK, doJSON(t, mux, http.MethodGet, "/api/v1/tasks", nil).Code)
	require.Equal(t, http.StatusCreated, doJSON(t, mux, http.MethodPost, "/api/v1/tasks", map[string]any{"title": "x"}).Code)
	require.Equal(t, http.StatusOK, doJSON(t, mux, http.MethodGet, "/api/v1/tasks/1", nil).Code)
	require.Equal(t, http.StatusOK, doJSON(t, mux, http.MethodPatch, "/api/v1/tasks/1", map[string]any{"title": "y"}).Code)
	require.Equal(t, http.StatusNoContent, doJSON(t, mux, http.MethodDelete, "/api/v1/tasks/1", nil).Code)
	assert.Len(t, seen, 5)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tasks", nil)
	req.Header.Set("X-Test-Reject", "yes")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "middleware must be able to answer instead of the handler")
}

// Hosting a database must not cost the operator local access to it: the
// repository opens SQLite in WAL with a busy timeout, so a co-located local
// client reads and writes the same file while the host is serving from it.
func TestColocatedLocalClientSharesTheDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "todo.db")
	mux := NewMux(newTestService(t, dbPath), nil)
	local := newTestService(t, dbPath)

	created := createTaskViaAPI(t, mux, map[string]any{"title": "added over HTTP"})
	fromLocal, err := local.GetTask(context.Background(), created.ID)
	require.NoError(t, err, "local client reading the host's database")
	assert.Equal(t, "added over HTTP", fromLocal.Title)

	addedLocally, err := local.AddTask(context.Background(), todo.NewTask{Title: "added locally"})
	require.NoError(t, err, "local client writing the host's database")

	rec := doJSON(t, mux, http.MethodGet, taskPath(addedLocally.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "added locally", decodeTask(t, rec).Title)
}
