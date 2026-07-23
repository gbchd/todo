package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/service/todo"
)

func openTestRepo(t *testing.T) *SQLiteRepository {
	t.Helper()
	path := filepath.Join(t.TempDir(), "todo.db")
	repo, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestOpen_Migrates(t *testing.T) {
	repo := openTestRepo(t)
	var version int
	require.NoError(t, repo.db.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version))
	assert.Equal(t, 1, version)
}

func TestOpen_ReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.db")
	ctx := context.Background()

	repo1, err := Open(ctx, path)
	require.NoError(t, err, "first Open")
	created, err := repo1.Create(ctx, todo.Task{
		Title: "persisted", Status: todo.StatusOpen, Priority: todo.PriorityNone,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	require.NoError(t, err)
	repo1.Close()

	repo2, err := Open(ctx, path)
	require.NoError(t, err, "second Open")
	defer repo2.Close()

	got, err := repo2.Get(ctx, created.ID)
	require.NoError(t, err, "Get after reopen")
	assert.Equal(t, "persisted", got.Title)
}

func TestCreateGetRoundTrip(t *testing.T) {
	repo := openTestRepo(t)
	ctx := context.Background()

	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	in := todo.Task{
		Title:       "Buy milk",
		Description: "2%",
		Status:      todo.StatusOpen,
		Priority:    todo.PriorityHigh,
		DueDate:     &due,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	created, err := repo.Create(ctx, in)
	require.NoError(t, err)
	assert.NotZero(t, created.ID)

	got, err := repo.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, in.Title, got.Title)
	assert.Equal(t, in.Description, got.Description)
	assert.Equal(t, in.Status, got.Status)
	assert.Equal(t, in.Priority, got.Priority)
	require.NotNil(t, got.DueDate)
	assert.True(t, got.DueDate.Equal(due), "DueDate = %v, want %v", got.DueDate, due)
	assert.True(t, got.CreatedAt.Equal(now), "CreatedAt = %v, want %v", got.CreatedAt, now)
	assert.True(t, got.UpdatedAt.Equal(now), "UpdatedAt = %v, want %v", got.UpdatedAt, now)
	assert.Nil(t, got.CompletedAt)
}

func TestGet_NotFound(t *testing.T) {
	repo := openTestRepo(t)
	_, err := repo.Get(context.Background(), 999)
	require.ErrorIs(t, err, todo.ErrNotFound)
}

func TestUpdate(t *testing.T) {
	repo := openTestRepo(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	created, _ := repo.Create(ctx, todo.Task{
		Title: "orig", Status: todo.StatusOpen, Priority: todo.PriorityNone,
		CreatedAt: now, UpdatedAt: now,
	})

	created.Title = "changed"
	created.Status = todo.StatusDone
	completedAt := now.Add(time.Hour)
	created.CompletedAt = &completedAt

	updated, err := repo.Update(ctx, created)
	require.NoError(t, err)
	assert.Equal(t, "changed", updated.Title)
	assert.Equal(t, todo.StatusDone, updated.Status)
	require.NotNil(t, updated.CompletedAt)
	assert.True(t, updated.CompletedAt.Equal(completedAt), "CompletedAt = %v, want %v", updated.CompletedAt, completedAt)
}

func TestUpdate_NotFound(t *testing.T) {
	repo := openTestRepo(t)
	_, err := repo.Update(context.Background(), todo.Task{ID: 999, Title: "x", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	require.ErrorIs(t, err, todo.ErrNotFound)
}

func TestDelete(t *testing.T) {
	repo := openTestRepo(t)
	ctx := context.Background()
	now := time.Now()
	created, err := repo.Create(ctx, todo.Task{Title: "x", Status: todo.StatusOpen, Priority: todo.PriorityNone, CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)

	require.NoError(t, repo.Delete(ctx, created.ID))
	_, err = repo.Get(ctx, created.ID)
	assert.ErrorIs(t, err, todo.ErrNotFound)
}

func TestDelete_NotFound(t *testing.T) {
	repo := openTestRepo(t)
	err := repo.Delete(context.Background(), 999)
	require.ErrorIs(t, err, todo.ErrNotFound)
}

func TestList_Filters(t *testing.T) {
	repo := openTestRepo(t)
	ctx := context.Background()
	now := time.Now()

	openHigh, _ := repo.Create(ctx, todo.Task{Title: "open-high", Status: todo.StatusOpen, Priority: todo.PriorityHigh, CreatedAt: now, UpdatedAt: now})
	repo.Create(ctx, todo.Task{Title: "done-low", Status: todo.StatusDone, Priority: todo.PriorityLow, CreatedAt: now, UpdatedAt: now})

	status := todo.StatusOpen
	got, err := repo.List(ctx, todo.TaskFilter{Status: &status})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, openHigh.ID, got[0].ID)
}

func TestList_DueDateRange(t *testing.T) {
	repo := openTestRepo(t)
	ctx := context.Background()
	now := time.Now()

	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	base := todo.Task{Status: todo.StatusOpen, Priority: todo.PriorityNone, CreatedAt: now, UpdatedAt: now}
	earlyTask := base
	earlyTask.Title, earlyTask.DueDate = "early", &early
	repo.Create(ctx, earlyTask)
	lateTask := base
	lateTask.Title, lateTask.DueDate = "late", &late
	late1, err := repo.Create(ctx, lateTask)
	require.NoError(t, err)
	noDueTask := base
	noDueTask.Title = "no-due"
	repo.Create(ctx, noDueTask)

	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got, err := repo.List(ctx, todo.TaskFilter{DueAfter: &cutoff})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, late1.ID, got[0].ID)
}
