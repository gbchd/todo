package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/transport/host/hosttest"
	"github.com/gbchd/todo/internal/transport/web"
)

// TestWebUI_OverARemoteBackend runs `todo serve`'s own API — the local web
// UI's, not the host's — on top of this adapter, which is exactly how a paired
// device serves its browser. The test lives here rather than beside the web
// transport because it is a statement about this adapter: the web UI is
// unchanged, and is not asked to know anything.
func TestWebUI_OverARemoteBackend(t *testing.T) {
	h := hosttest.StartFresh(t)
	mux := web.NewMux(todo.NewService(New(h.URL, h.Token)))

	call := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	created := call(http.MethodPost, "/api/tasks", `{"title":"Buy milk","priority":"high"}`)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())

	var task struct {
		ID     int64  `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &task))
	assert.Equal(t, "Buy milk", task.Title)

	sub := call(http.MethodPost, "/api/tasks", `{"title":"Find a shop","parent_id":`+strconv.FormatInt(task.ID, 10)+`}`)
	require.Equal(t, http.StatusCreated, sub.Code, sub.Body.String())

	listed := call(http.MethodGet, "/api/tasks?parent=none", "")
	require.Equal(t, http.StatusOK, listed.Code)
	var rows []struct {
		ID         int64 `json:"id"`
		ChildCount int   `json:"child_count"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &rows))
	require.Len(t, rows, 1, "the tri-state parent filter must survive both hops")
	assert.Equal(t, 1, rows[0].ChildCount, "the subtask rollup must reach the browser")

	patched := call(http.MethodPatch, "/api/tasks/"+strconv.FormatInt(task.ID, 10), `{"status":"done"}`)
	require.Equal(t, http.StatusOK, patched.Code, patched.Body.String())
	require.NoError(t, json.Unmarshal(patched.Body.Bytes(), &task))
	assert.Equal(t, "done", task.Status)

	missing := call(http.MethodGet, "/api/tasks/9999", "")
	assert.Equal(t, http.StatusNotFound, missing.Code, "a missing task must still be a 404, not a 500")

	rejected := call(http.MethodPost, "/api/tasks", `{"title":"   "}`)
	assert.Equal(t, http.StatusBadRequest, rejected.Code, "a rejected field must still be a 400")
}

// TestWebUI_SurfacesAnUnreachableHost: the browser must be told, rather than
// shown an empty list that looks like a task list with nothing in it.
func TestWebUI_SurfacesAnUnreachableHost(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()

	mux := web.NewMux(todo.NewService(New(url, "id.secret")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/tasks", nil))

	assert.GreaterOrEqual(t, rec.Code, 500, "an unreachable host is not an empty task list")
	assert.NotEqual(t, "[]\n", rec.Body.String())

	// The browser renders whatever is in "error", so what the adapter said has
	// to survive the hop: the frontend shows that string and, because the
	// request failed rather than returning nothing, keeps its "no tasks yet"
	// message for a list that is genuinely empty.
	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, body.Error, "cannot reach the todo host")
	assert.Contains(t, body.Error, url, "the page must name the host that did not answer")
}
