package todo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addChild is the two-step every subtask test starts with: a top-level parent
// and one Subtask hanging off it.
func addChild(t *testing.T, svc *Service, parentID int64, title string) Task {
	t.Helper()
	child, err := svc.AddTask(context.Background(), NewTask{Title: title, ParentID: &parentID})
	require.NoError(t, err, "add subtask %q", title)
	return child
}

func TestAddTask_WithParent(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, err := svc.AddTask(ctx, NewTask{Title: "parent"})
	require.NoError(t, err)

	child := addChild(t, svc, parent.ID, "child")
	require.NotNil(t, child.ParentID)
	assert.Equal(t, parent.ID, *child.ParentID)
}

// A parent id that names no task is a rejected field, not a missing task —
// otherwise the transports answer a bad parent with a 404 that reads as
// "the task you asked for does not exist".
func TestAddTask_ParentNotFound(t *testing.T) {
	svc, _ := newTestService()
	missing := int64(999)

	_, err := svc.AddTask(context.Background(), NewTask{Title: "orphan", ParentID: &missing})
	assertParentValidationError(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
}

func TestAddTask_ParentIsItselfASubtaskRejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	child := addChild(t, svc, parent.ID, "child")

	_, err := svc.AddTask(ctx, NewTask{Title: "grandchild", ParentID: &child.ID})
	assertParentValidationError(t, err)
}

func TestUpdateTask_DemoteAndPromote(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	loose, _ := svc.AddTask(ctx, NewTask{Title: "loose"})

	demoted, err := svc.UpdateTask(ctx, loose.ID, TaskPatch{ParentID: Set(&parent.ID)})
	require.NoError(t, err, "demote")
	require.NotNil(t, demoted.ParentID)
	assert.Equal(t, parent.ID, *demoted.ParentID)

	promoted, err := svc.UpdateTask(ctx, loose.ID, TaskPatch{ParentID: Set[*int64](nil)})
	require.NoError(t, err, "promote")
	assert.Nil(t, promoted.ParentID)
}

func TestUpdateTask_ParentUntouchedWhenUnset(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	child := addChild(t, svc, parent.ID, "child")

	got, err := svc.UpdateTask(ctx, child.ID, TaskPatch{Title: Set("renamed")})
	require.NoError(t, err)
	require.NotNil(t, got.ParentID, "an unset ParentID must not promote the subtask")
	assert.Equal(t, parent.ID, *got.ParentID)
}

func TestUpdateTask_TaskWithSubtasksCannotBecomeOne(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	addChild(t, svc, parent.ID, "child")
	other, _ := svc.AddTask(ctx, NewTask{Title: "other"})

	_, err := svc.UpdateTask(ctx, parent.ID, TaskPatch{ParentID: Set(&other.ID)})
	assertParentValidationError(t, err)
}

func TestUpdateTask_SelfParentRejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	task, _ := svc.AddTask(ctx, NewTask{Title: "task"})

	_, err := svc.UpdateTask(ctx, task.ID, TaskPatch{ParentID: Set(&task.ID)})
	assertParentValidationError(t, err)
}

func TestListTasks_TopLevelOnly(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	addChild(t, svc, parent.ID, "child")

	got, err := svc.ListTasks(ctx, TaskFilter{ParentID: Set[*int64](nil)})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, parent.ID, got[0].ID)
}

func TestListTasks_ChildrenOfParent(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	child := addChild(t, svc, parent.ID, "child")
	svc.AddTask(ctx, NewTask{Title: "unrelated"})

	got, err := svc.ListTasks(ctx, TaskFilter{ParentID: Set(&parent.ID)})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, child.ID, got[0].ID)
}

// Revealing subtasks groups them under their parent rather than sorting them
// globally, so --sort priority --all is not a globally descending column.
func TestListTasks_RevealedSubtasksGroupUnderParent(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	lowParent, _ := svc.AddTask(ctx, NewTask{Title: "low parent", Priority: PriorityLow})
	child := addChild(t, svc, lowParent.ID, "low parent's child")
	highLoose, _ := svc.AddTask(ctx, NewTask{Title: "high loose", Priority: PriorityHigh})

	got, err := svc.ListTasks(ctx, TaskFilter{SortBy: SortPriority})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []int64{highLoose.ID, lowParent.ID, child.ID}, ids(got),
		"subtask must follow its parent, not sort globally by priority")
}

// A subtask whose parent is filtered out has nothing to group under, so it
// keeps its own sorted position rather than vanishing.
func TestListTasks_OrphanedSubtaskKeepsItsOwnPosition(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	child := addChild(t, svc, parent.ID, "child")
	svc.CompleteTask(ctx, child.ID)

	done := StatusDone
	got, err := svc.ListTasks(ctx, TaskFilter{Status: &done})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, child.ID, got[0].ID)
}

func TestListTasks_ParentRollsUpChildren(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	overdue := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	doneChild := addChild(t, svc, parent.ID, "done child")
	svc.AddTask(ctx, NewTask{Title: "overdue child", ParentID: &parent.ID, DueDate: &overdue})
	svc.CompleteTask(ctx, doneChild.ID)

	got, err := svc.ListTasks(ctx, TaskFilter{ParentID: Set[*int64](nil)})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].ChildCount)
	assert.Equal(t, 1, got[0].DoneChildCount)
	assert.True(t, got[0].AnyChildOverdue, "parent must flag an overdue subtask hidden beneath it")
}

func TestDeleteTask_CascadesToSubtasks(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	parent, _ := svc.AddTask(ctx, NewTask{Title: "parent"})
	child := addChild(t, svc, parent.ID, "child")

	require.NoError(t, svc.DeleteTask(ctx, parent.ID))
	_, err := svc.GetTask(ctx, child.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func assertParentValidationError(t *testing.T, err error) {
	t.Helper()
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "parent", verr.Field)
}

func ids(tasks []Task) []int64 {
	out := make([]int64, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}
