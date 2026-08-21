package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeTasks(t *testing.T, rec *httptest.ResponseRecorder) []taskDTO {
	t.Helper()
	var tasks []taskDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tasks), "decode tasks (body=%s)", rec.Body.String())
	return tasks
}

// createParentAndChild posts a parent and one subtask, returning both.
func createParentAndChild(t *testing.T, mux http.Handler) (parent, child taskDTO) {
	t.Helper()
	rec := doJSON(t, mux, "POST", "/api/tasks", map[string]any{"title": "parent"})
	require.Equal(t, http.StatusCreated, rec.Code, "create parent")
	parent = decodeTask(t, rec)

	rec = doJSON(t, mux, "POST", "/api/tasks", map[string]any{"title": "child", "parent_id": parent.ID})
	require.Equal(t, http.StatusCreated, rec.Code, "create child (body=%s)", rec.Body.String())
	return parent, decodeTask(t, rec)
}

func TestCreateTask_WithParent(t *testing.T) {
	mux := newTestMux(t)
	parent, child := createParentAndChild(t, mux)

	require.NotNil(t, child.ParentID)
	assert.Equal(t, parent.ID, *child.ParentID)
}

func TestCreateTask_ParentIsItselfASubtaskRejected(t *testing.T) {
	mux := newTestMux(t)
	_, child := createParentAndChild(t, mux)

	rec := doJSON(t, mux, "POST", "/api/tasks", map[string]any{"title": "grandchild", "parent_id": child.ID})
	assert.Equal(t, http.StatusBadRequest, rec.Code, "one-level violations are a 400, not a 500")
}

// A parent_id naming no task is a bad request, not a missing resource.
func TestCreateTask_UnknownParentIsBadRequest(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "POST", "/api/tasks", map[string]any{"title": "orphan", "parent_id": 999})
	assert.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
}

func TestListTasks_ParentQueryParam(t *testing.T) {
	mux := newTestMux(t)
	parent, child := createParentAndChild(t, mux)

	all := decodeTasks(t, doJSON(t, mux, "GET", "/api/tasks", nil))
	assert.Len(t, all, 2, "an absent ?parent= must stay unconstrained")

	topLevel := decodeTasks(t, doJSON(t, mux, "GET", "/api/tasks?parent=none", nil))
	require.Len(t, topLevel, 1)
	assert.Equal(t, parent.ID, topLevel[0].ID)
	assert.Equal(t, 1, topLevel[0].ChildCount, "a hidden subtask still rolls up onto its parent")

	children := decodeTasks(t, doJSON(t, mux, "GET", "/api/tasks?parent="+itoa(parent.ID), nil))
	require.Len(t, children, 1)
	assert.Equal(t, child.ID, children[0].ID)
}

func TestListTasks_InvalidParentQueryParam(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "GET", "/api/tasks?parent=bogus", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPatchTask_ParentIDDemotesAndPromotes(t *testing.T) {
	mux := newTestMux(t)
	rec := doJSON(t, mux, "POST", "/api/tasks", map[string]any{"title": "parent"})
	parent := decodeTask(t, rec)
	rec = doJSON(t, mux, "POST", "/api/tasks", map[string]any{"title": "loose"})
	loose := decodeTask(t, rec)

	rec = doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(loose.ID), map[string]any{"parent_id": parent.ID})
	require.Equal(t, http.StatusOK, rec.Code, "demote (body=%s)", rec.Body.String())
	got := decodeTask(t, rec)
	require.NotNil(t, got.ParentID)
	assert.Equal(t, parent.ID, *got.ParentID)

	rec = doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(loose.ID), map[string]any{"parent_id": nil})
	require.Equal(t, http.StatusOK, rec.Code, "promote")
	assert.Nil(t, decodeTask(t, rec).ParentID, "an explicit null promotes to top level")
}

// An absent parent_id key must leave the relationship alone — the distinction
// a plain pointer could not express.
func TestPatchTask_AbsentParentIDLeavesSubtaskAlone(t *testing.T) {
	mux := newTestMux(t)
	parent, child := createParentAndChild(t, mux)

	rec := doJSON(t, mux, "PATCH", "/api/tasks/"+itoa(child.ID), map[string]any{"title": "renamed"})
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeTask(t, rec)
	require.NotNil(t, got.ParentID)
	assert.Equal(t, parent.ID, *got.ParentID)
}

func TestDeleteTask_CascadesToSubtasks(t *testing.T) {
	mux := newTestMux(t)
	parent, child := createParentAndChild(t, mux)

	rec := doJSON(t, mux, "DELETE", "/api/tasks/"+itoa(parent.ID), nil)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = doJSON(t, mux, "GET", "/api/tasks/"+itoa(child.ID), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
