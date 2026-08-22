package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addSubtaskToFirstTask drives the only path that creates a Subtask: enter the
// parent's detail view, then "a".
func addSubtaskToFirstTask(t *testing.T, m model) model {
	t.Helper()
	m = send(t, m, "enter", "a")
	require.Equal(t, modeForm, m.mode, "detail-view 'a' must open the add form")
	require.NotNil(t, m.form.parentID, "the form must carry the parent it was opened from")

	for _, r := range "a subtask" {
		m = send(t, m, string(r))
	}
	return send(t, m, "enter")
}

func TestModel_AddSubtaskFromDetailView(t *testing.T) {
	m := newTestModel(t, layoutList)
	parentID := m.tasks[0].ID

	m = addSubtaskToFirstTask(t, m)

	assert.Equal(t, modeDetail, m.mode, "want to land back on the parent that created it")
	require.Len(t, m.tasks, 2, "a new subtask must not appear in the top-level list")
	assert.Equal(t, 1, m.tasks[0].ChildCount, "the parent's row must roll the subtask up")
	require.Len(t, m.detailChildren(), 1)
	require.NotNil(t, m.detailChildren()[0].ParentID)
	assert.Equal(t, parentID, *m.detailChildren()[0].ParentID)
}

func TestModel_ToggleRevealsSubtasks(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = addSubtaskToFirstTask(t, m)
	m = send(t, m, "esc")

	require.False(t, m.showSubtasks, "subtasks start hidden on every launch")
	require.Len(t, m.tasks, 2)

	m = send(t, m, "s")
	require.True(t, m.showSubtasks)
	require.Len(t, m.tasks, 3, "'s' must reveal the subtask")

	m = send(t, m, "s")
	assert.Len(t, m.tasks, 2, "'s' toggles back off")
}

// A Subtask is one level deep, so its detail view offers no "add subtask".
func TestModel_SubtaskDetailViewCannotAddSubtask(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = addSubtaskToFirstTask(t, m)
	m = send(t, m, "esc", "s")

	// Move the cursor onto the revealed subtask, which sorts under its parent.
	m = send(t, m, "down")
	selected, ok := m.selectedTask()
	require.True(t, ok)
	require.NotNil(t, selected.ParentID, "cursor should be on the subtask")

	m = send(t, m, "enter", "a")
	assert.Equal(t, modeDetail, m.mode, "'a' must be inert on a subtask's detail view")
}

func TestModel_DeleteParentRemovesSubtasks(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = addSubtaskToFirstTask(t, m)
	m = send(t, m, "esc")

	m = send(t, m, "d", "y")
	require.NoError(t, m.err)

	m = send(t, m, "s")
	assert.Len(t, m.tasks, 1, "deleting the parent must take its subtask with it")
}

func TestRollup(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = addSubtaskToFirstTask(t, m)
	m = send(t, m, "esc")

	assert.Equal(t, " (0/1)", rollup(m.tasks[0]))
	assert.Empty(t, rollup(m.tasks[1]), "a task with no subtasks rolls up nothing")
}

// The split layout renders the detail pane on every browse frame, so the
// subtask list has to follow the cursor — otherwise moving off a parent leaves
// its subtasks displayed under the next task's heading.
func TestModel_SubtaskListFollowsCursor(t *testing.T) {
	m := newTestModel(t, layoutSplit)
	m = addSubtaskToFirstTask(t, m)
	m = send(t, m, "esc")

	require.Len(t, m.detailChildren(), 1, "cursor starts on the parent")

	m = send(t, m, "down")
	selected, ok := m.selectedTask()
	require.True(t, ok)
	require.Equal(t, "second task", selected.Title)
	assert.Empty(t, m.detailChildren(), "a task with no subtasks must not show the previous task's")

	m = send(t, m, "up")
	assert.Len(t, m.detailChildren(), 1, "moving back restores the parent's subtasks")
}
