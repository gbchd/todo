package todo

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestService() (*Service, *fakeRepository, *time.Time) {
	repo := newFakeRepository()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clock := &now
	svc := &Service{repo: repo, now: func() time.Time { return *clock }}
	return svc, repo, clock
}

func TestAddTask(t *testing.T) {
	ctx := context.Background()
	svc, _, now := newTestService()

	got, err := svc.AddTask(ctx, NewTask{Title: "Buy milk"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if got.ID == 0 {
		t.Errorf("expected non-zero id")
	}
	if got.Status != StatusOpen {
		t.Errorf("Status = %q, want open", got.Status)
	}
	if got.Priority != PriorityNone {
		t.Errorf("Priority = %q, want none", got.Priority)
	}
	if !got.CreatedAt.Equal(*now) || !got.UpdatedAt.Equal(*now) {
		t.Errorf("timestamps not set to clock time")
	}
}

func TestAddTask_EmptyTitle(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.AddTask(context.Background(), NewTask{Title: "   "})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if verr.Field != "title" {
		t.Errorf("Field = %q, want title", verr.Field)
	}
}

func TestAddTask_InvalidPriority(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.AddTask(context.Background(), NewTask{Title: "x", Priority: "urgent"})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if verr.Field != "priority" {
		t.Errorf("Field = %q, want priority", verr.Field)
	}
}

func TestAddTask_NormalizesDueDate(t *testing.T) {
	svc, _, _ := newTestService()
	due := time.Date(2026, 8, 1, 15, 30, 0, 0, time.FixedZone("x", 3600))
	got, err := svc.AddTask(context.Background(), NewTask{Title: "x", DueDate: &due})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if got.DueDate == nil {
		t.Fatalf("DueDate is nil")
	}
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if !got.DueDate.Equal(want) {
		t.Errorf("DueDate = %v, want %v", got.DueDate, want)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.GetTask(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateTask_PartialPatch(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "orig", Description: "desc", Priority: PriorityLow})

	got, err := svc.UpdateTask(ctx, created.ID, TaskPatch{Priority: Set(PriorityHigh)})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if got.Title != "orig" {
		t.Errorf("Title = %q, want unchanged orig", got.Title)
	}
	if got.Description != "desc" {
		t.Errorf("Description = %q, want unchanged desc", got.Description)
	}
	if got.Priority != PriorityHigh {
		t.Errorf("Priority = %q, want high", got.Priority)
	}
}

func TestUpdateTask_ClearDueDate(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	created, _ := svc.AddTask(ctx, NewTask{Title: "x", DueDate: &due})

	got, err := svc.UpdateTask(ctx, created.ID, TaskPatch{DueDate: Set[*time.Time](nil)})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if got.DueDate != nil {
		t.Errorf("DueDate = %v, want nil", got.DueDate)
	}
}

func TestUpdateTask_ClearDescription(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "x", Description: "desc"})

	got, err := svc.UpdateTask(ctx, created.ID, TaskPatch{Description: Set("")})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if got.Description != "" {
		t.Errorf("Description = %q, want empty", got.Description)
	}
}

func TestUpdateTask_EmptyTitleRejected(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "x"})

	_, err := svc.UpdateTask(ctx, created.ID, TaskPatch{Title: Set("  ")})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
}

func TestUpdateTask_NotFound(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.UpdateTask(context.Background(), 999, TaskPatch{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestStatusLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _, clock := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "x"})

	*clock = clock.Add(time.Hour)
	started, err := svc.StartTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if started.Status != StatusInProgress {
		t.Errorf("Status = %q, want in-progress", started.Status)
	}

	*clock = clock.Add(time.Hour)
	done, err := svc.CompleteTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if done.Status != StatusDone {
		t.Errorf("Status = %q, want done", done.Status)
	}
	if done.CompletedAt == nil || !done.CompletedAt.Equal(*clock) {
		t.Errorf("CompletedAt = %v, want %v", done.CompletedAt, clock)
	}

	*clock = clock.Add(time.Hour)
	reopened, err := svc.ReopenTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("ReopenTask: %v", err)
	}
	if reopened.Status != StatusOpen {
		t.Errorf("Status = %q, want open", reopened.Status)
	}
	if reopened.CompletedAt != nil {
		t.Errorf("CompletedAt = %v, want nil after reopen", reopened.CompletedAt)
	}
}

func TestDeleteTask(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "x"})

	if err := svc.DeleteTask(ctx, created.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := svc.GetTask(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	svc, _, _ := newTestService()
	err := svc.DeleteTask(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListTasks_DefaultSort(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()

	dueLater := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dueSoon := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	open1, _ := svc.AddTask(ctx, NewTask{Title: "open no due", Priority: PriorityLow})
	open2, _ := svc.AddTask(ctx, NewTask{Title: "open due later", DueDate: &dueLater})
	inProg, _ := svc.AddTask(ctx, NewTask{Title: "in progress", DueDate: &dueSoon})
	svc.StartTask(ctx, inProg.ID)

	got, err := svc.ListTasks(ctx, TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	// in-progress group first, then open group ordered by due date (nil last).
	if got[0].ID != inProg.ID {
		t.Errorf("got[0] = %q, want in-progress task first", got[0].Title)
	}
	if got[1].ID != open2.ID {
		t.Errorf("got[1] = %q, want open-with-due-date second", got[1].Title)
	}
	if got[2].ID != open1.ID {
		t.Errorf("got[2] = %q, want no-due-date task last", got[2].Title)
	}
}

func TestListTasks_SortByPriority(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()

	low, _ := svc.AddTask(ctx, NewTask{Title: "low", Priority: PriorityLow})
	high, _ := svc.AddTask(ctx, NewTask{Title: "high", Priority: PriorityHigh})

	got, err := svc.ListTasks(ctx, TaskFilter{SortBy: SortPriority})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if got[0].ID != high.ID || got[1].ID != low.ID {
		t.Errorf("sort by priority order wrong: %v", got)
	}
}

func TestListTasks_FilterByStatus(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()

	svc.AddTask(ctx, NewTask{Title: "open"})
	done, _ := svc.AddTask(ctx, NewTask{Title: "done"})
	svc.CompleteTask(ctx, done.ID)

	status := StatusDone
	got, err := svc.ListTasks(ctx, TaskFilter{Status: &status})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 1 || got[0].ID != done.ID {
		t.Errorf("got = %v, want only done task", got)
	}
}
