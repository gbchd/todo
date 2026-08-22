package tui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/repository/remote"
	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/transport/host/hosttest"
)

// newRemoteModel builds the model a paired device's TUI runs: the same model,
// over the same Service, over an HTTP repository pointed at an in-process
// host. Nothing in the TUI is told which one it got.
func newRemoteModel(t *testing.T, layout layoutKind) (model, *todo.Service) {
	t.Helper()
	h := hosttest.StartFresh(t)
	svc := todo.NewService(remote.New(h.URL, h.Token))

	ctx := context.Background()
	parent, err := svc.AddTask(ctx, todo.NewTask{Title: "first task", Priority: todo.PriorityHigh})
	require.NoError(t, err, "seed")
	_, err = svc.AddTask(ctx, todo.NewTask{Title: "second task"})
	require.NoError(t, err, "seed")
	_, err = svc.AddTask(ctx, todo.NewTask{Title: "a subtask", ParentID: &parent.ID})
	require.NoError(t, err, "seed")

	return newModel(ctx, svc, layout), svc
}

// TestModel_OverARemoteBackend drives the interactions that touch the
// repository — browsing, the status cycle, deleting, revealing Subtasks — and
// asserts the TUI behaves as it does on a local database, which it must,
// because the seam is below it.
func TestModel_OverARemoteBackend(t *testing.T) {
	m, svc := newRemoteModel(t, layoutList)

	first, ok := m.selectedTask()
	require.True(t, ok, "initial selection")
	assert.Equal(t, "first task", first.Title)
	assert.Equal(t, 1, first.ChildCount, "the subtask rollup must survive the wire")
	require.Len(t, m.tasks, 2, "subtasks stay hidden until revealed")

	m = send(t, m, " ")
	started, _ := m.selectedTask()
	assert.Equal(t, todo.StatusInProgress, started.Status, "status after one advance")

	stored, err := svc.GetTask(context.Background(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, todo.StatusInProgress, stored.Status, "the advance must have reached the host")

	m = send(t, m, "d", "y")
	assert.Len(t, m.tasks, 1, "after a confirmed delete")

	remaining, err := svc.ListTasks(context.Background(), todo.TaskFilter{})
	require.NoError(t, err)
	assert.Len(t, remaining, 1, "the delete must have cascaded on the host, not just in the view")
}

// TestModel_RemoteFailureKeepsTheSessionAlive: a host that stops answering
// mid-session must leave the TUI running with the failure on show, not take
// the session down with it.
func TestModel_RemoteFailureKeepsTheSessionAlive(t *testing.T) {
	m, _ := newRemoteModel(t, layoutList)
	require.NotEmpty(t, m.tasks)

	// The model keeps its Service; the repository underneath it is replaced by
	// one pointed at nothing, which is what a dropped network looks like from
	// inside a running session.
	m.svc = todo.NewService(remote.New("http://127.0.0.1:1", "id.secret"))
	m.reload()

	assert.Error(t, m.err, "the failure must be on show")
	assert.NotPanics(t, func() { _ = m.View() }, "the session must survive it")
}
