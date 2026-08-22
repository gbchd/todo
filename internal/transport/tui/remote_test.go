package tui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

	ctx := t.Context()
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

	stored, err := svc.GetTask(t.Context(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, todo.StatusInProgress, stored.Status, "the advance must have reached the host")

	m = send(t, m, "d", "y")
	assert.Len(t, m.tasks, 1, "after a confirmed delete")

	remaining, err := svc.ListTasks(t.Context(), todo.TaskFilter{})
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

// droppable is a repository that can be made to stop answering, which is what
// a network going away looks like from inside a session that is already
// running. Underneath it is the real HTTP adapter against a real host, so
// everything that still works while it is down is genuinely still working.
type droppable struct {
	todo.TaskRepository
	url  string
	down atomic.Bool
}

func (d *droppable) fail() error {
	return fmt.Errorf("%w at %s: connection refused", remote.ErrUnreachable, d.url)
}

func (d *droppable) Create(ctx context.Context, t todo.Task) (todo.Task, error) {
	if d.down.Load() {
		return todo.Task{}, d.fail()
	}
	return d.TaskRepository.Create(ctx, t)
}

func (d *droppable) Get(ctx context.Context, id int64) (todo.Task, error) {
	if d.down.Load() {
		return todo.Task{}, d.fail()
	}
	return d.TaskRepository.Get(ctx, id)
}

func (d *droppable) List(ctx context.Context, f todo.TaskFilter) ([]todo.Task, error) {
	if d.down.Load() {
		return nil, d.fail()
	}
	return d.TaskRepository.List(ctx, f)
}

func (d *droppable) Delete(ctx context.Context, id int64) error {
	if d.down.Load() {
		return d.fail()
	}
	return d.TaskRepository.Delete(ctx, id)
}

func (d *droppable) UpdateWith(ctx context.Context, id int64, mutate func(todo.Task) (todo.Task, error)) (todo.Task, error) {
	if d.down.Load() {
		return todo.Task{}, d.fail()
	}
	return d.TaskRepository.UpdateWith(ctx, id, mutate)
}

// newDroppableModel is newRemoteModel with a switch on the wire.
func newDroppableModel(t *testing.T) (model, *droppable) {
	t.Helper()
	h := hosttest.StartFresh(t)
	repo := &droppable{TaskRepository: remote.New(h.URL, h.Token), url: h.URL}
	svc := todo.NewService(repo)

	ctx := t.Context()
	_, err := svc.AddTask(ctx, todo.NewTask{Title: "first task", Priority: todo.PriorityHigh})
	require.NoError(t, err, "seed")
	_, err = svc.AddTask(ctx, todo.NewTask{Title: "second task"})
	require.NoError(t, err, "seed")

	return newModel(ctx, svc, layoutList), repo
}

// TestRun_RefusesToStartWhenTheHostIsUnreachable: a session that never got a
// first answer has nothing to show, so it does not open. The caller gets the
// error and the terminal it was launched from.
func TestRun_RefusesToStartWhenTheHostIsUnreachable(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()

	done := make(chan error, 1)
	go func() {
		done <- Run(t.Context(), todo.NewService(remote.New(url, "id.secret")),
			"list", strings.NewReader(""), io.Discard)
	}()

	select {
	case err := <-done:
		require.Error(t, err, "the TUI must not have started")
		assert.ErrorIs(t, err, remote.ErrUnreachable)
		assert.Contains(t, err.Error(), url, "the message must name the host, as the CLI's does")
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return: a TUI with no task list must refuse to start, not wait")
	}
}

// TestModel_AFailedWriteKeepsTheSessionAliveAndTheNextReadRepaints is the
// subtle half of the same rule. A dropped packet mid-session must cost the
// user the write and nothing else: the tasks stay on screen, the failure is on
// the status line, and when the host comes back the next read replaces both —
// which it can only do because the model caches nothing and re-reads
// everything.
func TestModel_AFailedWriteKeepsTheSessionAliveAndTheNextReadRepaints(t *testing.T) {
	m, wire := newDroppableModel(t)
	require.Len(t, m.tasks, 2)
	before, ok := m.selectedTask()
	require.True(t, ok)
	require.Equal(t, todo.StatusOpen, before.Status)

	wire.down.Store(true)

	// A status change: the write fails, and the session keeps everything it
	// had — the rows, the selection, and its ability to draw a frame.
	m = send(t, m, " ")
	require.Error(t, m.err, "the failed write must be reported")
	assert.ErrorIs(t, m.err, remote.ErrUnreachable)
	assert.Len(t, m.tasks, 2, "a failed write must not empty the list")
	still, ok := m.selectedTask()
	require.True(t, ok, "the selection must survive")
	assert.Equal(t, before.Status, still.Status, "the row must not show a change that was never made")

	frame := m.View()
	assert.Contains(t, frame, "first task", "the session must still be showing the tasks")
	assert.Contains(t, frame, wire.url, "the status line must name the host that did not answer")

	// A delete: same rule, and the task the host never deleted is still listed.
	m = send(t, m, "d", "y")
	require.Error(t, m.err)
	assert.Len(t, m.tasks, 2, "a delete the host never took must not remove the row")
	assert.Equal(t, modeBrowse, m.mode, "the confirmation must not be left open")

	// The host comes back. The next read is what repaints, and it clears the
	// failure with it: nothing had to be invalidated, because nothing was kept.
	wire.down.Store(false)
	m.reload()
	assert.NoError(t, m.err, "a successful read must clear the failure")
	assert.NotContains(t, m.View(), wire.url, "and take the status line with it")
	assert.Len(t, m.tasks, 2)
}
