package tui

import (
	"context"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func seededService(t *testing.T) *todo.Service {
	t.Helper()
	ctx := context.Background()
	repo, err := repository.Open(ctx, t.TempDir()+"/todo.db")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	seedTasks := []todo.Task{
		{Title: "Write report", Status: todo.StatusOpen, Priority: todo.PriorityHigh, DueDate: &due, CreatedAt: now, UpdatedAt: now},
		{Title: "Review PR", Status: todo.StatusInProgress, Priority: todo.PriorityNone, CreatedAt: now, UpdatedAt: now},
		{Title: "Ship release", Status: todo.StatusDone, Priority: todo.PriorityLow, CreatedAt: now, UpdatedAt: now},
	}
	for _, task := range seedTasks {
		if _, err := repo.Create(ctx, task); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return todo.NewService(repo)
}

func runGolden(t *testing.T, layout layoutKind) []byte {
	t.Helper()
	svc := seededService(t)
	m := newModel(context.Background(), svc, layout)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	time.Sleep(300 * time.Millisecond)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if err := tm.Quit(); err != nil {
		t.Fatalf("quit: %v", err)
	}

	out := tm.FinalOutput(t, teatest.WithFinalTimeout(3*time.Second))
	bts, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("read final output: %v", err)
	}
	return bts
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
