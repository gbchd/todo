package tui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func newTestModel(t *testing.T, layout layoutKind) model {
	t.Helper()
	ctx := context.Background()
	repo, err := repository.Open(ctx, filepath.Join(t.TempDir(), "todo.db"))
	require.NoError(t, err, "open repo")
	t.Cleanup(func() { repo.Close() })
	svc := todo.NewService(repo)

	_, err = svc.AddTask(ctx, todo.NewTask{Title: "first task", Priority: todo.PriorityHigh})
	require.NoError(t, err, "seed")
	_, err = svc.AddTask(ctx, todo.NewTask{Title: "second task"})
	require.NoError(t, err, "seed")

	return newModel(ctx, svc, layout)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(key(k))
		m = next.(model)
	}
	return m
}

func TestModel_NavigateCursor(t *testing.T) {
	m := newTestModel(t, layoutList)
	require.Equal(t, 0, m.cursor, "initial cursor")
	m = send(t, m, "down")
	assert.Equal(t, 1, m.cursor, "cursor after down")
	m = send(t, m, "down") // clamps at last index
	assert.Equal(t, 1, m.cursor, "cursor after second down, want clamped")
	m = send(t, m, "up")
	assert.Equal(t, 0, m.cursor, "cursor after up")
}

func TestModel_EnterOpensDetailEscBack(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "enter")
	require.Equal(t, modeDetail, m.mode)
	m = send(t, m, "esc")
	require.Equal(t, modeBrowse, m.mode)
}

func TestModel_AddTask(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "a")
	require.Equal(t, modeForm, m.mode)
	assert.Zero(t, m.form.editingID, "editingID want 0 for add")

	for _, r := range "third task" {
		m = send(t, m, string(r))
	}
	m = send(t, m, "enter")

	require.Equal(t, modeBrowse, m.mode, "mode after save")
	require.Len(t, m.tasks, 3)
}

func TestModel_AddTask_EmptyTitleRejected(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "a")
	m = send(t, m, "enter")
	require.Equal(t, modeForm, m.mode, "want still modeForm on validation error")
	assert.NotEmpty(t, m.form.err, "expected form error for empty title")
}

func TestModel_EditCyclePriority(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "e")
	require.Equal(t, modeForm, m.mode)
	m = send(t, m, "tab", "tab") // title -> description -> priority
	require.Equal(t, fieldPriority, m.form.focus)
	before := m.form.priority
	m = send(t, m, "right")
	assert.NotEqual(t, before, m.form.priority, "priority did not change after right")
}

func TestModel_DeleteConfirmFlow(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "d")
	require.Equal(t, modeConfirmDelete, m.mode)
	m = send(t, m, "n")
	require.Equal(t, modeBrowse, m.mode, "mode after decline")
	require.Len(t, m.tasks, 2, "unchanged")

	m = send(t, m, "d", "y")
	require.Len(t, m.tasks, 1, "after confirmed delete")
}

func TestModel_AdvanceStatus(t *testing.T) {
	m := newTestModel(t, layoutList)
	t0, ok := m.selectedTask()
	require.True(t, ok)
	require.Equal(t, todo.StatusOpen, t0.Status, "expected first task open, got %+v", t0)

	m = send(t, m, " ")
	t1, _ := m.selectedTask()
	assert.Equal(t, todo.StatusInProgress, t1.Status, "status after 1 advance")

	m = send(t, m, " ")
	t2, _ := m.selectedTask()
	assert.Equal(t, todo.StatusDone, t2.Status, "status after 2 advances")

	m = send(t, m, " ")
	t3, _ := m.selectedTask()
	assert.Equal(t, todo.StatusOpen, t3.Status, "status after 3 advances, want wrapped")
}

func TestModel_Kanban_ColumnNavigation(t *testing.T) {
	m := newTestModel(t, layoutKanban)
	require.Equal(t, 0, m.column, "initial column")
	m = send(t, m, "right")
	assert.Equal(t, 1, m.column, "column after right")
	m = send(t, m, "left")
	assert.Equal(t, 0, m.column, "column after left")
}

func TestModel_Kanban_MoveCardColumn(t *testing.T) {
	m := newTestModel(t, layoutKanban)
	task, ok := m.selectedTask()
	require.True(t, ok, "expected a selected task in open column")

	m = send(t, m, "L")
	require.Equal(t, 1, m.column, "column after L, want in-progress")

	moved, err := m.svc.GetTask(m.ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, todo.StatusInProgress, moved.Status)
}

func TestModel_Quit(t *testing.T) {
	m := newTestModel(t, layoutList)
	next, cmd := m.Update(key("q"))
	m2 := next.(model)
	require.True(t, m2.quit)
	require.NotNil(t, cmd, "expected tea.Quit command")
	assert.Empty(t, m2.View(), "View() after quit should be empty")
}

func TestView_AllLayoutsRenderTaskTitles(t *testing.T) {
	for _, layout := range []layoutKind{layoutList, layoutSplit, layoutKanban} {
		m := newTestModel(t, layout)
		m.width, m.height = 80, 24
		out := m.View()
		assert.Contains(t, out, "first task", "layout %v: View() missing task title", layout)
	}
}

func TestParseLayout(t *testing.T) {
	cases := map[string]layoutKind{
		"list": layoutList, "split": layoutSplit, "kanban": layoutKanban, "": layoutList, "bogus": layoutList,
	}
	for in, want := range cases {
		assert.Equal(t, want, ParseLayout(in), "ParseLayout(%q)", in)
	}
}
