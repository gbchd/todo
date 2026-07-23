package todo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService() (*Service, *time.Time) {
	repo := newFakeRepository()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clock := &now
	svc := &Service{repo: repo, now: func() time.Time { return *clock }}
	return svc, clock
}

func TestAddTask(t *testing.T) {
	ctx := context.Background()
	svc, now := newTestService()

	got, err := svc.AddTask(ctx, NewTask{Title: "Buy milk"})
	require.NoError(t, err)
	assert.NotZero(t, got.ID)
	assert.Equal(t, StatusOpen, got.Status)
	assert.Equal(t, PriorityNone, got.Priority)
	assert.True(t, got.CreatedAt.Equal(*now) && got.UpdatedAt.Equal(*now), "timestamps not set to clock time")
}

func TestAddTask_EmptyTitle(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.AddTask(context.Background(), NewTask{Title: "   "})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "title", verr.Field)
}

func TestAddTask_InvalidPriority(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.AddTask(context.Background(), NewTask{Title: "x", Priority: "urgent"})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "priority", verr.Field)
}

func TestAddTask_NormalizesDueDate(t *testing.T) {
	svc, _ := newTestService()
	due := time.Date(2026, 8, 1, 15, 30, 0, 0, time.FixedZone("x", 3600))
	got, err := svc.AddTask(context.Background(), NewTask{Title: "x", DueDate: &due})
	require.NoError(t, err)
	require.NotNil(t, got.DueDate)
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	assert.True(t, got.DueDate.Equal(want), "DueDate = %v, want %v", got.DueDate, want)
}

func TestGetTask_NotFound(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.GetTask(context.Background(), 999)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUpdateTask_PartialPatch(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "orig", Description: "desc", Priority: PriorityLow})

	got, err := svc.UpdateTask(ctx, created.ID, TaskPatch{Priority: Set(PriorityHigh)})
	require.NoError(t, err)
	assert.Equal(t, "orig", got.Title)
	assert.Equal(t, "desc", got.Description)
	assert.Equal(t, PriorityHigh, got.Priority)
}

func TestUpdateTask_ClearDueDate(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	created, _ := svc.AddTask(ctx, NewTask{Title: "x", DueDate: &due})

	got, err := svc.UpdateTask(ctx, created.ID, TaskPatch{DueDate: Set[*time.Time](nil)})
	require.NoError(t, err)
	assert.Nil(t, got.DueDate)
}

func TestUpdateTask_ClearDescription(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "x", Description: "desc"})

	got, err := svc.UpdateTask(ctx, created.ID, TaskPatch{Description: Set("")})
	require.NoError(t, err)
	assert.Equal(t, "", got.Description)
}

func TestUpdateTask_EmptyTitleRejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "x"})

	_, err := svc.UpdateTask(ctx, created.ID, TaskPatch{Title: Set("  ")})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
}

func TestUpdateTask_NotFound(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.UpdateTask(context.Background(), 999, TaskPatch{})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestStatusLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, clock := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "x"})

	*clock = clock.Add(time.Hour)
	started, err := svc.StartTask(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusInProgress, started.Status)

	*clock = clock.Add(time.Hour)
	done, err := svc.CompleteTask(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDone, done.Status)
	require.NotNil(t, done.CompletedAt)
	assert.True(t, done.CompletedAt.Equal(*clock), "CompletedAt = %v, want %v", done.CompletedAt, clock)

	*clock = clock.Add(time.Hour)
	reopened, err := svc.ReopenTask(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusOpen, reopened.Status)
	assert.Nil(t, reopened.CompletedAt)
}

func TestDeleteTask(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	created, _ := svc.AddTask(ctx, NewTask{Title: "x"})

	require.NoError(t, svc.DeleteTask(ctx, created.ID))
	_, err := svc.GetTask(ctx, created.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeleteTask_NotFound(t *testing.T) {
	svc, _ := newTestService()
	err := svc.DeleteTask(context.Background(), 999)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestListTasks_DefaultSort(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	dueLater := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dueSoon := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	open1, _ := svc.AddTask(ctx, NewTask{Title: "open no due", Priority: PriorityLow})
	open2, _ := svc.AddTask(ctx, NewTask{Title: "open due later", DueDate: &dueLater})
	inProg, _ := svc.AddTask(ctx, NewTask{Title: "in progress", DueDate: &dueSoon})
	svc.StartTask(ctx, inProg.ID)

	got, err := svc.ListTasks(ctx, TaskFilter{})
	require.NoError(t, err)
	require.Len(t, got, 3)
	// in-progress group first, then open group ordered by due date (nil last).
	assert.Equal(t, inProg.ID, got[0].ID, "want in-progress task first")
	assert.Equal(t, open2.ID, got[1].ID, "want open-with-due-date second")
	assert.Equal(t, open1.ID, got[2].ID, "want no-due-date task last")
}

func TestListTasks_SortByPriority(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	low, _ := svc.AddTask(ctx, NewTask{Title: "low", Priority: PriorityLow})
	high, _ := svc.AddTask(ctx, NewTask{Title: "high", Priority: PriorityHigh})

	got, err := svc.ListTasks(ctx, TaskFilter{SortBy: SortPriority})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, high.ID, got[0].ID)
	assert.Equal(t, low.ID, got[1].ID)
}

func TestListTasks_FilterByStatus(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	svc.AddTask(ctx, NewTask{Title: "open"})
	done, _ := svc.AddTask(ctx, NewTask{Title: "done"})
	svc.CompleteTask(ctx, done.ID)

	status := StatusDone
	got, err := svc.ListTasks(ctx, TaskFilter{Status: &status})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, done.ID, got[0].ID)
}

func TestListTasks_InvalidStatusFilter(t *testing.T) {
	svc, _ := newTestService()
	status := Status("bogus")
	_, err := svc.ListTasks(context.Background(), TaskFilter{Status: &status})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "status", verr.Field)
}

func TestListTasks_InvalidPriorityFilter(t *testing.T) {
	svc, _ := newTestService()
	priority := Priority("urgent")
	_, err := svc.ListTasks(context.Background(), TaskFilter{Priority: &priority})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "priority", verr.Field)
}

func TestListTasks_InvalidSortKey(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.ListTasks(context.Background(), TaskFilter{SortBy: SortKey("bogus")})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "sort", verr.Field)
}
