package tui

import (
	"context"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func seededService(t *testing.T) *todo.Service {
	t.Helper()
	ctx := context.Background()
	repo, err := repository.Open(ctx, t.TempDir()+"/todo.db")
	require.NoError(t, err, "open repo")
	t.Cleanup(func() { repo.Close() })

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	seedTasks := []todo.Task{
		{Title: "Write report", Status: todo.StatusOpen, Priority: todo.PriorityHigh, DueDate: &due, CreatedAt: now, UpdatedAt: now},
		{Title: "Review PR", Status: todo.StatusInProgress, Priority: todo.PriorityNone, CreatedAt: now, UpdatedAt: now},
		{Title: "Ship release", Status: todo.StatusDone, Priority: todo.PriorityLow, CreatedAt: now, UpdatedAt: now},
	}
	for _, task := range seedTasks {
		_, err := repo.Create(ctx, task)
		require.NoError(t, err, "seed")
	}
	return todo.NewService(repo)
}

// serviceWithSubtask seeds a Parent Task carrying one open Subtask, so the
// reveal toggle has something to reveal.
func serviceWithSubtask(t *testing.T) *todo.Service {
	t.Helper()
	ctx := context.Background()
	repo, err := repository.Open(ctx, t.TempDir()+"/todo.db")
	require.NoError(t, err, "open repo")
	t.Cleanup(func() { repo.Close() })

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	parent, err := repo.Create(ctx, todo.Task{
		Title: "Ship release", Status: todo.StatusOpen, Priority: todo.PriorityHigh,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err, "seed parent")
	_, err = repo.Create(ctx, todo.Task{
		Title: "Write changelog", Status: todo.StatusOpen, Priority: todo.PriorityLow,
		DueDate: &due, ParentID: &parent.ID, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err, "seed subtask")
	return todo.NewService(repo)
}

// renderGolden captures the model's rendered frame. Golden files pin what a
// layout *looks* like; the key handling that gets the model into a given state
// is asserted directly in model_test.go and subtask_test.go, so nothing here
// has to drive keys and wait for frames to settle.
func renderGolden(t *testing.T, m model) []byte {
	t.Helper()
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	time.Sleep(300 * time.Millisecond)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	require.NoError(t, tm.Quit(), "quit")

	out := tm.FinalOutput(t, teatest.WithFinalTimeout(3*time.Second))
	bts, err := io.ReadAll(out)
	require.NoError(t, err, "read final output")
	return bts
}

func runGolden(t *testing.T, layout layoutKind) []byte {
	t.Helper()
	return renderGolden(t, newModel(context.Background(), seededService(t), layout))
}

func TestGolden_ListLayout(t *testing.T) {
	teatest.RequireEqualOutput(t, runGolden(t, layoutList))
}

func TestGolden_SplitLayout(t *testing.T) {
	teatest.RequireEqualOutput(t, runGolden(t, layoutSplit))
}

func TestGolden_KanbanLayout(t *testing.T) {
	teatest.RequireEqualOutput(t, runGolden(t, layoutKanban))
}

// The list hides subtasks by default, rolling them up onto the parent's row.
func TestGolden_ListSubtasksHidden(t *testing.T) {
	m := newModel(context.Background(), serviceWithSubtask(t), layoutList)
	teatest.RequireEqualOutput(t, renderGolden(t, m))
}

// Revealed, they render indented under the parent they were rolled up onto.
func TestGolden_ListSubtasksRevealed(t *testing.T) {
	m := newModel(context.Background(), serviceWithSubtask(t), layoutList)
	m.showSubtasks = true
	m.reload()
	teatest.RequireEqualOutput(t, renderGolden(t, m))
}
