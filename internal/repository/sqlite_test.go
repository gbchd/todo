package repository

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gbchd/todo/internal/service/todo"
)

func openTestRepo(t *testing.T) *SQLiteRepository {
	t.Helper()
	path := filepath.Join(t.TempDir(), "todo.db")
	repo, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestOpen_Migrates(t *testing.T) {
	repo := openTestRepo(t)
	var version int
	if err := repo.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Errorf("user_version = %d, want 1", version)
	}
}

func TestOpen_ReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.db")
	ctx := context.Background()

	repo1, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	created, err := repo1.Create(ctx, todo.Task{
		Title: "persisted", Status: todo.StatusOpen, Priority: todo.PriorityNone,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	repo1.Close()

	repo2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer repo2.Close()

	got, err := repo2.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Title != "persisted" {
		t.Errorf("Title = %q, want persisted", got.Title)
	}
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
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected non-zero id")
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != in.Title || got.Description != in.Description || got.Status != in.Status || got.Priority != in.Priority {
		t.Errorf("got = %+v, want fields matching %+v", got, in)
	}
	if got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Errorf("DueDate = %v, want %v", got.DueDate, due)
	}
	if !got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
		t.Errorf("timestamps = %v/%v, want %v", got.CreatedAt, got.UpdatedAt, now)
	}
	if got.CompletedAt != nil {
		t.Errorf("CompletedAt = %v, want nil", got.CompletedAt)
	}
}

func TestGet_NotFound(t *testing.T) {
	repo := openTestRepo(t)
	_, err := repo.Get(context.Background(), 999)
	if !errors.Is(err, todo.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
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
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "changed" || updated.Status != todo.StatusDone {
		t.Errorf("Update didn't persist: %+v", updated)
	}
	if updated.CompletedAt == nil || !updated.CompletedAt.Equal(completedAt) {
		t.Errorf("CompletedAt = %v, want %v", updated.CompletedAt, completedAt)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	repo := openTestRepo(t)
	_, err := repo.Update(context.Background(), todo.Task{ID: 999, Title: "x", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if !errors.Is(err, todo.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	repo := openTestRepo(t)
	ctx := context.Background()
	now := time.Now()
	created, err := repo.Create(ctx, todo.Task{Title: "x", Status: todo.StatusOpen, Priority: todo.PriorityNone, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, created.ID); !errors.Is(err, todo.ErrNotFound) {
		t.Errorf("Get after delete: %v, want ErrNotFound", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	repo := openTestRepo(t)
	err := repo.Delete(context.Background(), 999)
	if !errors.Is(err, todo.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestList_Filters(t *testing.T) {
	repo := openTestRepo(t)
	ctx := context.Background()
	now := time.Now()

	openHigh, _ := repo.Create(ctx, todo.Task{Title: "open-high", Status: todo.StatusOpen, Priority: todo.PriorityHigh, CreatedAt: now, UpdatedAt: now})
	repo.Create(ctx, todo.Task{Title: "done-low", Status: todo.StatusDone, Priority: todo.PriorityLow, CreatedAt: now, UpdatedAt: now})

	status := todo.StatusOpen
	got, err := repo.List(ctx, todo.TaskFilter{Status: &status})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != openHigh.ID {
		t.Errorf("got = %v, want only open-high", got)
	}
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
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	noDueTask := base
	noDueTask.Title = "no-due"
	repo.Create(ctx, noDueTask)

	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got, err := repo.List(ctx, todo.TaskFilter{DueAfter: &cutoff})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != late1.ID {
		t.Errorf("got = %v, want only late task", got)
	}
}
