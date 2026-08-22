// Package todotest holds the shared contract suite for the
// todo.TaskRepository port. It lives in a non-test file so that every
// package shipping an implementation of the port can import it and be held
// to the same behaviour, rather than each adapter growing its own private
// idea of what the port promises.
package todotest

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/service/todo"
)

// NewRepository builds a fresh, empty repository for one contract case. It is
// called once per case, so implementations must not share state between the
// repositories they return; use t for temporary directories and cleanup.
type NewRepository func(t *testing.T) todo.TaskRepository

// RunTaskRepositoryContract runs every case of the TaskRepository contract
// against the implementation newRepo builds, each in its own subtest with its
// own empty repository. An implementation that passes behaves like every
// other one as far as a caller of the port can tell.
func RunTaskRepositoryContract(t *testing.T, newRepo NewRepository) {
	t.Helper()
	for _, tc := range taskRepositoryContract {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, newRepo(t))
		})
	}
}

// contractCase is one named statement about the port. Cases are added to
// taskRepositoryContract below; nothing else needs touching.
type contractCase struct {
	name string
	run  func(t *testing.T, repo todo.TaskRepository)
}

// fixtureNow is the timestamp every fixture task is stamped with, so that a
// failure message never contains a value that changes between runs.
var fixtureNow = time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)

// newTask returns a valid, minimal top-level task ready to be handed to
// Create. Cases copy it and overwrite the one or two fields they care about.
func newTask(title string) todo.Task {
	return todo.Task{
		Title:     title,
		Status:    todo.StatusOpen,
		Priority:  todo.PriorityNone,
		CreatedAt: fixtureNow,
		UpdatedAt: fixtureNow,
	}
}

// mustCreate creates t0 and fails the test if the repository refuses it;
// fixtures are setup, not the thing under assertion.
func mustCreate(t *testing.T, repo todo.TaskRepository, t0 todo.Task) todo.Task {
	t.Helper()
	created, err := repo.Create(t.Context(), t0)
	require.NoError(t, err, "create fixture %q", t0.Title)
	return created
}

// ids returns the ids of tasks, in order, for comparing list results without
// asserting on every field of every row.
func ids(tasks []todo.Task) []int64 {
	out := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.ID)
	}
	return out
}

var taskRepositoryContract = []contractCase{
	{"create and get round-trip every field", func(t *testing.T, repo todo.TaskRepository) {
		due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		in := newTask("Buy milk")
		in.Description = "2%"
		in.Priority = todo.PriorityHigh
		in.DueDate = &due

		created := mustCreate(t, repo, in)
		assert.NotZero(t, created.ID, "Create must assign an id")

		got, err := repo.Get(t.Context(), created.ID)
		require.NoError(t, err)
		assert.Equal(t, in.Title, got.Title)
		assert.Equal(t, in.Description, got.Description)
		assert.Equal(t, in.Status, got.Status)
		assert.Equal(t, in.Priority, got.Priority)
		require.NotNil(t, got.DueDate)
		assert.True(t, got.DueDate.Equal(due), "DueDate = %v, want %v", got.DueDate, due)
		assert.True(t, got.CreatedAt.Equal(fixtureNow), "CreatedAt = %v, want %v", got.CreatedAt, fixtureNow)
		assert.True(t, got.UpdatedAt.Equal(fixtureNow), "UpdatedAt = %v, want %v", got.UpdatedAt, fixtureNow)
		assert.Nil(t, got.CompletedAt)
	}},

	{"get reports a missing task as ErrNotFound", func(t *testing.T, repo todo.TaskRepository) {
		_, err := repo.Get(t.Context(), 999)
		require.ErrorIs(t, err, todo.ErrNotFound)
	}},

	{"update with applies the mutation and returns the stored task", func(t *testing.T, repo todo.TaskRepository) {
		created := mustCreate(t, repo, newTask("orig"))

		completedAt := fixtureNow.Add(time.Hour)
		updated, err := repo.UpdateWith(t.Context(), created.ID, func(task todo.Task) (todo.Task, error) {
			task.Title = "changed"
			task.Status = todo.StatusDone
			task.CompletedAt = &completedAt
			return task, nil
		})
		require.NoError(t, err)
		assert.Equal(t, "changed", updated.Title)
		assert.Equal(t, todo.StatusDone, updated.Status)
		require.NotNil(t, updated.CompletedAt)
		assert.True(t, updated.CompletedAt.Equal(completedAt), "CompletedAt = %v, want %v", updated.CompletedAt, completedAt)

		got, err := repo.Get(t.Context(), created.ID)
		require.NoError(t, err, "re-read after update")
		assert.Equal(t, "changed", got.Title, "the mutation must be persisted, not just returned")
	}},

	{"update with reports a missing task as ErrNotFound", func(t *testing.T, repo todo.TaskRepository) {
		_, err := repo.UpdateWith(t.Context(), 999, func(task todo.Task) (todo.Task, error) { return task, nil })
		require.ErrorIs(t, err, todo.ErrNotFound)
	}},

	{"update with surfaces the mutation's own error and writes nothing", func(t *testing.T, repo todo.TaskRepository) {
		created := mustCreate(t, repo, newTask("orig"))

		want := &todo.ValidationError{Field: "title", Message: "must not be empty"}
		_, err := repo.UpdateWith(t.Context(), created.ID, func(task todo.Task) (todo.Task, error) {
			task.Title = "changed"
			return task, want
		})
		require.ErrorIs(t, err, want)

		got, err := repo.Get(t.Context(), created.ID)
		require.NoError(t, err)
		assert.Equal(t, "orig", got.Title, "a rejected mutation must leave the stored task untouched")
	}},

	{"update with is atomic under concurrent writers", func(t *testing.T, repo todo.TaskRepository) {
		created := mustCreate(t, repo, newTask("contended"))

		// Each writer appends one character to the description. A lost
		// update — two writers reading the same row before either writes —
		// shows up as a description shorter than the number of writers,
		// whatever order they happened to run in.
		const writers = 8
		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			errs []error
		)
		for range writers {
			wg.Go(func() {
				_, err := repo.UpdateWith(t.Context(), created.ID, func(task todo.Task) (todo.Task, error) {
					task.Description += "x"
					return task, nil
				})
				if err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		require.Empty(t, errs, "concurrent UpdateWith calls must all succeed")

		got, err := repo.Get(t.Context(), created.ID)
		require.NoError(t, err)
		assert.Len(t, got.Description, writers, "every concurrent update must be observed by the next one")
	}},

	{"delete removes the task", func(t *testing.T, repo todo.TaskRepository) {
		created := mustCreate(t, repo, newTask("x"))

		require.NoError(t, repo.Delete(t.Context(), created.ID))
		_, err := repo.Get(t.Context(), created.ID)
		assert.ErrorIs(t, err, todo.ErrNotFound)
	}},

	{"delete reports a missing task as ErrNotFound", func(t *testing.T, repo todo.TaskRepository) {
		err := repo.Delete(t.Context(), 999)
		require.ErrorIs(t, err, todo.ErrNotFound)
	}},

	// Deleting a Parent Task takes its Subtasks with it. On SQLite this is the
	// one failure mode a fake repository cannot catch: PRAGMA foreign_keys
	// defaults off per connection, so ON DELETE CASCADE parses, migrates, and
	// then silently orphans children.
	{"delete cascades to subtasks", func(t *testing.T, repo todo.TaskRepository) {
		parent := mustCreate(t, repo, newTask("parent"))
		child := newTask("child")
		child.ParentID = &parent.ID
		created := mustCreate(t, repo, child)

		require.NoError(t, repo.Delete(t.Context(), parent.ID))

		_, err := repo.Get(t.Context(), created.ID)
		assert.ErrorIs(t, err, todo.ErrNotFound, "a subtask must be deleted along with its parent")
	}},

	{"a nil due date and a nil parent round-trip as nil", func(t *testing.T, repo todo.TaskRepository) {
		created := mustCreate(t, repo, newTask("bare"))
		assert.Nil(t, created.DueDate, "Create must not invent a zero due date")
		assert.Nil(t, created.ParentID, "Create must not invent a zero parent id")

		got, err := repo.Get(t.Context(), created.ID)
		require.NoError(t, err)
		assert.Nil(t, got.DueDate)
		assert.Nil(t, got.ParentID)

		listed, err := repo.List(t.Context(), todo.TaskFilter{})
		require.NoError(t, err)
		require.Len(t, listed, 1)
		assert.Nil(t, listed[0].DueDate)
		assert.Nil(t, listed[0].ParentID)
	}},

	{"clearing a due date and a parent stores nil, not a zero value", func(t *testing.T, repo todo.TaskRepository) {
		parent := mustCreate(t, repo, newTask("parent"))
		due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		child := newTask("child")
		child.DueDate, child.ParentID = &due, &parent.ID
		created := mustCreate(t, repo, child)

		cleared, err := repo.UpdateWith(t.Context(), created.ID, func(task todo.Task) (todo.Task, error) {
			task.DueDate, task.ParentID = nil, nil
			return task, nil
		})
		require.NoError(t, err)
		assert.Nil(t, cleared.DueDate)
		assert.Nil(t, cleared.ParentID)

		got, err := repo.Get(t.Context(), created.ID)
		require.NoError(t, err)
		assert.Nil(t, got.DueDate, "a cleared due date must read back as nil")
		assert.Nil(t, got.ParentID, "a promoted subtask must read back with no parent")
	}},

	{"list is unconstrained when the parent filter is unset", func(t *testing.T, repo todo.TaskRepository) {
		parent := mustCreate(t, repo, newTask("parent"))
		child := newTask("child")
		child.ParentID = &parent.ID
		created := mustCreate(t, repo, child)

		got, err := repo.List(t.Context(), todo.TaskFilter{})
		require.NoError(t, err)
		assert.Equal(t, []int64{parent.ID, created.ID}, ids(got),
			"an unset ParentID must match parents and subtasks alike")
	}},

	{"list returns only top-level tasks when the parent filter is set to nil", func(t *testing.T, repo todo.TaskRepository) {
		parent := mustCreate(t, repo, newTask("parent"))
		child := newTask("child")
		child.ParentID = &parent.ID
		mustCreate(t, repo, child)

		got, err := repo.List(t.Context(), todo.TaskFilter{ParentID: todo.Set[*int64](nil)})
		require.NoError(t, err)
		assert.Equal(t, []int64{parent.ID}, ids(got))
	}},

	{"list returns only the subtasks of the parent the filter names", func(t *testing.T, repo todo.TaskRepository) {
		parent := mustCreate(t, repo, newTask("parent"))
		other := mustCreate(t, repo, newTask("other parent"))
		child := newTask("child")
		child.ParentID = &parent.ID
		created := mustCreate(t, repo, child)
		otherChild := newTask("other child")
		otherChild.ParentID = &other.ID
		mustCreate(t, repo, otherChild)

		got, err := repo.List(t.Context(), todo.TaskFilter{ParentID: todo.Set(&parent.ID)})
		require.NoError(t, err)
		assert.Equal(t, []int64{created.ID}, ids(got))
	}},

	{"list filters by status", func(t *testing.T, repo todo.TaskRepository) {
		openHigh := newTask("open-high")
		openHigh.Priority = todo.PriorityHigh
		wanted := mustCreate(t, repo, openHigh)
		doneLow := newTask("done-low")
		doneLow.Status, doneLow.Priority = todo.StatusDone, todo.PriorityLow
		mustCreate(t, repo, doneLow)

		status := todo.StatusOpen
		got, err := repo.List(t.Context(), todo.TaskFilter{Status: &status})
		require.NoError(t, err)
		assert.Equal(t, []int64{wanted.ID}, ids(got))
	}},

	{"list filters by priority", func(t *testing.T, repo todo.TaskRepository) {
		high := newTask("high")
		high.Priority = todo.PriorityHigh
		wanted := mustCreate(t, repo, high)
		mustCreate(t, repo, newTask("none"))

		priority := todo.PriorityHigh
		got, err := repo.List(t.Context(), todo.TaskFilter{Priority: &priority})
		require.NoError(t, err)
		assert.Equal(t, []int64{wanted.ID}, ids(got))
	}},

	// DueBefore and DueAfter are strictly exclusive and exclude tasks with no
	// due date at all, rather than sorting those to one end of the range.
	{"list filters by due date range", func(t *testing.T, repo todo.TaskRepository) {
		early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		late := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
		earlyTask := newTask("early")
		earlyTask.DueDate = &early
		earlyCreated := mustCreate(t, repo, earlyTask)
		lateTask := newTask("late")
		lateTask.DueDate = &late
		lateCreated := mustCreate(t, repo, lateTask)
		mustCreate(t, repo, newTask("no-due"))

		cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		after, err := repo.List(t.Context(), todo.TaskFilter{DueAfter: &cutoff})
		require.NoError(t, err)
		assert.Equal(t, []int64{lateCreated.ID}, ids(after), "DueAfter excludes tasks with no due date")

		before, err := repo.List(t.Context(), todo.TaskFilter{DueBefore: &cutoff})
		require.NoError(t, err)
		assert.Equal(t, []int64{earlyCreated.ID}, ids(before), "DueBefore excludes tasks with no due date")
	}},

	{"reads populate the derived subtask rollup fields", func(t *testing.T, repo todo.TaskRepository) {
		parent := mustCreate(t, repo, newTask("parent"))
		overdue := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		openChild := newTask("overdue child")
		openChild.ParentID, openChild.DueDate = &parent.ID, &overdue
		mustCreate(t, repo, openChild)
		doneChild := newTask("done child")
		doneChild.ParentID, doneChild.DueDate, doneChild.Status = &parent.ID, &overdue, todo.StatusDone
		mustCreate(t, repo, doneChild)

		got, err := repo.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.Equal(t, 2, got.ChildCount)
		assert.Equal(t, 1, got.DoneChildCount)
		assert.True(t, got.AnyChildOverdue, "an open subtask past its due date makes the parent flag overdue")

		listed, err := repo.List(t.Context(), todo.TaskFilter{ParentID: todo.Set[*int64](nil)})
		require.NoError(t, err)
		require.Len(t, listed, 1)
		assert.Equal(t, 2, listed[0].ChildCount, "List must roll up exactly as Get does")
		assert.Equal(t, 1, listed[0].DoneChildCount)
		assert.True(t, listed[0].AnyChildOverdue)
	}},

	{"a parent whose only overdue subtask is done is not flagged overdue", func(t *testing.T, repo todo.TaskRepository) {
		parent := mustCreate(t, repo, newTask("settled parent"))
		overdue := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		doneChild := newTask("done child")
		doneChild.ParentID, doneChild.DueDate, doneChild.Status = &parent.ID, &overdue, todo.StatusDone
		mustCreate(t, repo, doneChild)

		got, err := repo.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, got.ChildCount)
		assert.False(t, got.AnyChildOverdue, "a done subtask is never counted as overdue")
	}},

	{"writes ignore the derived subtask rollup fields", func(t *testing.T, repo todo.TaskRepository) {
		bogus := newTask("parent")
		bogus.ChildCount, bogus.DoneChildCount, bogus.AnyChildOverdue = 99, 98, true
		parent := mustCreate(t, repo, bogus)
		assert.Equal(t, 0, parent.ChildCount, "Create must derive the rollup, not echo it back")
		assert.Equal(t, 0, parent.DoneChildCount)
		assert.False(t, parent.AnyChildOverdue)

		child := newTask("child")
		child.ParentID = &parent.ID
		mustCreate(t, repo, child)

		updated, err := repo.UpdateWith(t.Context(), parent.ID, func(task todo.Task) (todo.Task, error) {
			task.ChildCount, task.DoneChildCount, task.AnyChildOverdue = 99, 98, true
			return task, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, updated.ChildCount, "UpdateWith must re-derive the rollup, not store it")
		assert.Equal(t, 0, updated.DoneChildCount)
		assert.False(t, updated.AnyChildOverdue)

		got, err := repo.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, got.ChildCount)
		assert.Equal(t, 0, got.DoneChildCount)
		assert.False(t, got.AnyChildOverdue)
	}},

	// The two validation cases below run through a Service rather than calling
	// the port directly: rejecting a bad title or a bad parent is domain logic,
	// and an implementation of the port satisfies the contract by letting that
	// logic reach the caller with its Field and Message intact — matched with
	// errors.As, never by comparing error strings.
	{"an empty title is rejected as a title validation error", func(t *testing.T, repo todo.TaskRepository) {
		_, err := todo.NewService(repo).AddTask(t.Context(), todo.NewTask{Title: "   "})

		var verr *todo.ValidationError
		require.ErrorAs(t, err, &verr)
		assert.Equal(t, "title", verr.Field)
		assert.Equal(t, "must not be empty", verr.Message)
	}},

	{"a parent that is itself a subtask is rejected as a parent validation error", func(t *testing.T, repo todo.TaskRepository) {
		svc := todo.NewService(repo)
		parent, err := svc.AddTask(t.Context(), todo.NewTask{Title: "parent"})
		require.NoError(t, err)
		child, err := svc.AddTask(t.Context(), todo.NewTask{Title: "child", ParentID: &parent.ID})
		require.NoError(t, err)

		_, err = svc.AddTask(t.Context(), todo.NewTask{Title: "grandchild", ParentID: &child.ID})

		var verr *todo.ValidationError
		require.ErrorAs(t, err, &verr)
		assert.Equal(t, "parent", verr.Field)
		assert.Contains(t, verr.Message, "subtasks are only one level deep")
	}},
}
