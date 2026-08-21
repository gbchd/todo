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
	first, ok := m.selectedTask()
	require.True(t, ok, "initial selection")
	assert.Equal(t, "first task", first.Title, "initial selection")

	m = send(t, m, "down")
	second, ok := m.selectedTask()
	require.True(t, ok)
	assert.Equal(t, "second task", second.Title, "selection after down")

	m = send(t, m, "down") // clamps at last task
	afterClamp, ok := m.selectedTask()
	require.True(t, ok)
	assert.Equal(t, "second task", afterClamp.Title, "selection after second down, want clamped")

	m = send(t, m, "up")
	backToFirst, ok := m.selectedTask()
	require.True(t, ok)
	assert.Equal(t, "first task", backToFirst.Title, "selection after up")
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
	require.Equal(t, 0, m.kanbanColumn(), "initial column")
	m = send(t, m, "right")
	assert.Equal(t, 1, m.kanbanColumn(), "column after right")
	m = send(t, m, "left")
	assert.Equal(t, 0, m.kanbanColumn(), "column after left")
}

func TestModel_Kanban_MoveCardColumn(t *testing.T) {
	m := newTestModel(t, layoutKanban)
	task, ok := m.selectedTask()
	require.True(t, ok, "expected a selected task in open column")

	m = send(t, m, "L")
	require.Equal(t, 1, m.kanbanColumn(), "column after L, want in-progress")

	moved, err := m.svc.GetTask(m.ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, todo.StatusInProgress, moved.Status)
}

// TestModel_Kanban_AdvanceStatus_KeepsSelectionOnMovedCard reproduces a proven
// data-loss bug: an index-based selection model advanced or moved whichever
// card happened to sit at the old (row, column) position, not the one that
// was actually acted on, if the target column already held other cards. Here
// the open column holds two cards ([card3, card4]) and in-progress already
// holds two more ([card1, card2]) before the move — exactly the shape needed
// for a stale index to land on the wrong existing card.
func TestModel_Kanban_AdvanceStatus_KeepsSelectionOnMovedCard(t *testing.T) {
	m, ids := newKanbanFixture(t)

	// Select card4: index 1 within the open column [card3, card4].
	m.selectedID = ids[3]
	task, ok := m.selectedTask()
	require.True(t, ok)
	require.Equal(t, ids[3], task.ID)

	m.advanceStatus(task)

	selected, ok := m.selectedTask()
	require.True(t, ok, "selection must survive advanceStatus")
	assert.Equal(t, ids[3], selected.ID,
		"selection must stay on the card that was advanced, not slide onto a stale index in the target column")
}

// TestModel_Kanban_MoveCardColumn_KeepsSelectionOnMovedCard is the H/L
// counterpart of the advanceStatus regression above.
func TestModel_Kanban_MoveCardColumn_KeepsSelectionOnMovedCard(t *testing.T) {
	m, ids := newKanbanFixture(t)

	m.selectedID = ids[3]

	m.moveCardColumn(1) // H/L: open -> in-progress, which already holds card1, card2

	selected, ok := m.selectedTask()
	require.True(t, ok, "selection must survive moveCardColumn")
	assert.Equal(t, ids[3], selected.ID,
		"selection must stay on the card that was moved, not slide onto a stale index in the target column")
	assert.Equal(t, 1, m.kanbanColumn(), "moved card must land in the in-progress column")
}

// newKanbanFixture seeds exactly card1..card4, then moves card1 and card2 to
// in-progress, leaving card3 and card4 open: open=[card3, card4],
// in-progress=[card1, card2] — the same column shapes as the reported bug.
// Both columns end up with more than one card, so a stale index-based fixup
// and an identity-based one can disagree.
func newKanbanFixture(t *testing.T) (model, [4]int64) {
	t.Helper()
	ctx := context.Background()
	repo, err := repository.Open(ctx, filepath.Join(t.TempDir(), "todo.db"))
	require.NoError(t, err, "open repo")
	t.Cleanup(func() { repo.Close() })
	svc := todo.NewService(repo)

	var ids [4]int64
	titles := [4]string{"card1", "card2", "card3", "card4"}
	for i, title := range titles {
		task, err := svc.AddTask(ctx, todo.NewTask{Title: title})
		require.NoError(t, err, "seed %s", title)
		ids[i] = task.ID
	}
	_, err = svc.UpdateTask(ctx, ids[0], todo.TaskPatch{Status: todo.Set(todo.StatusInProgress)})
	require.NoError(t, err)
	_, err = svc.UpdateTask(ctx, ids[1], todo.TaskPatch{Status: todo.Set(todo.StatusInProgress)})
	require.NoError(t, err)

	return newModel(ctx, svc, layoutKanban), ids
}

// TestModel_Kanban_EmptyBoardDoesNotPanic covers the identity-based model's
// no-selection edge case: navigation must degrade gracefully, not panic, once
// every task is gone.
func TestModel_Kanban_EmptyBoardDoesNotPanic(t *testing.T) {
	m := newTestModel(t, layoutKanban)
	require.NoError(t, m.svc.DeleteTask(m.ctx, m.tasks[0].ID))
	require.NoError(t, m.svc.DeleteTask(m.ctx, m.tasks[1].ID))
	m.reload()

	_, ok := m.selectedTask()
	require.False(t, ok, "no task should be selected on an empty board")

	assert.NotPanics(t, func() { m.moveCursor(1) })
	assert.NotPanics(t, func() { m.moveColumn(1) })
	assert.NotPanics(t, func() { m.moveColumn(-1) })
	assert.NotPanics(t, func() { m.View() })

	_, ok = m.selectedTask()
	assert.False(t, ok, "still no selection after navigating an empty board")
}

// TestModel_Kanban_EmptyColumnNavigation covers switching into and back out
// of a column with no cards in it.
func TestModel_Kanban_EmptyColumnNavigation(t *testing.T) {
	m := newTestModel(t, layoutKanban) // both seeded tasks are open; in-progress is empty
	m = send(t, m, "right")
	require.Equal(t, 1, m.kanbanColumn())
	_, ok := m.selectedTask()
	assert.False(t, ok, "empty column should have no selection")

	assert.NotPanics(t, func() { m.moveCursor(1) })
	assert.NotPanics(t, func() { m.moveCursor(-1) })

	m = send(t, m, "left") // back to open, which still has 2 cards
	require.Equal(t, 0, m.kanbanColumn())
	_, ok = m.selectedTask()
	assert.True(t, ok, "moving back to a non-empty column must restore a selection")
}

// TestModel_DeletingSelectedTaskDegradesGracefully covers the list/split
// fallback when the selected task disappears out from under the cursor.
func TestModel_DeletingSelectedTaskDegradesGracefully(t *testing.T) {
	m := newTestModel(t, layoutList)
	first := m.tasks[0]
	require.NoError(t, m.svc.DeleteTask(m.ctx, first.ID))
	m.reload()

	sel, ok := m.selectedTask()
	require.True(t, ok, "must land on a neighbouring task, not lose selection")
	assert.NotEqual(t, first.ID, sel.ID)
	assert.Equal(t, "second task", sel.Title)
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
