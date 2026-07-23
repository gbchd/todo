package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func newTestModel(t *testing.T, layout layoutKind) model {
	t.Helper()
	ctx := context.Background()
	repo, err := repository.Open(ctx, filepath.Join(t.TempDir(), "todo.db"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	svc := todo.NewService(repo)

	if _, err := svc.AddTask(ctx, todo.NewTask{Title: "first task", Priority: todo.PriorityHigh}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.AddTask(ctx, todo.NewTask{Title: "second task"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

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
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	m = send(t, m, "down")
	if m.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", m.cursor)
	}
	m = send(t, m, "down") // clamps at last index
	if m.cursor != 1 {
		t.Errorf("cursor after second down = %d, want clamped 1", m.cursor)
	}
	m = send(t, m, "up")
	if m.cursor != 0 {
		t.Errorf("cursor after up = %d, want 0", m.cursor)
	}
}

func TestModel_EnterOpensDetailEscBack(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "enter")
	if m.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", m.mode)
	}
	m = send(t, m, "esc")
	if m.mode != modeBrowse {
		t.Fatalf("mode = %v, want modeBrowse", m.mode)
	}
}

func TestModel_AddTask(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "a")
	if m.mode != modeForm {
		t.Fatalf("mode = %v, want modeForm", m.mode)
	}
	if m.form.editingID != 0 {
		t.Errorf("editingID = %d, want 0 for add", m.form.editingID)
	}

	for _, r := range "third task" {
		m = send(t, m, string(r))
	}
	m = send(t, m, "enter")

	if m.mode != modeBrowse {
		t.Fatalf("mode after save = %v, want modeBrowse", m.mode)
	}
	if len(m.tasks) != 3 {
		t.Fatalf("len(tasks) = %d, want 3", len(m.tasks))
	}
}

func TestModel_AddTask_EmptyTitleRejected(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "a")
	m = send(t, m, "enter")
	if m.mode != modeForm {
		t.Fatalf("mode = %v, want still modeForm on validation error", m.mode)
	}
	if m.form.err == "" {
		t.Errorf("expected form error for empty title")
	}
}

func TestModel_EditCyclePriority(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "e")
	if m.mode != modeForm {
		t.Fatalf("mode = %v, want modeForm", m.mode)
	}
	m = send(t, m, "tab", "tab") // title -> description -> priority
	if m.form.focus != fieldPriority {
		t.Fatalf("focus = %v, want fieldPriority", m.form.focus)
	}
	before := m.form.priority
	m = send(t, m, "right")
	if m.form.priority == before {
		t.Errorf("priority did not change after right")
	}
}

func TestModel_DeleteConfirmFlow(t *testing.T) {
	m := newTestModel(t, layoutList)
	m = send(t, m, "d")
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want modeConfirmDelete", m.mode)
	}
	m = send(t, m, "n")
	if m.mode != modeBrowse {
		t.Fatalf("mode after decline = %v, want modeBrowse", m.mode)
	}
	if len(m.tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2 (unchanged)", len(m.tasks))
	}

	m = send(t, m, "d", "y")
	if len(m.tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1 after confirmed delete", len(m.tasks))
	}
}

func TestModel_AdvanceStatus(t *testing.T) {
	m := newTestModel(t, layoutList)
	t0, ok := m.selectedTask()
	if !ok || t0.Status != todo.StatusOpen {
		t.Fatalf("expected first task open, got %+v", t0)
	}

	m = send(t, m, " ")
	t1, _ := m.selectedTask()
	if t1.Status != todo.StatusInProgress {
		t.Errorf("status after 1 advance = %v, want in-progress", t1.Status)
	}

	m = send(t, m, " ")
	t2, _ := m.selectedTask()
	if t2.Status != todo.StatusDone {
		t.Errorf("status after 2 advances = %v, want done", t2.Status)
	}

	m = send(t, m, " ")
	t3, _ := m.selectedTask()
	if t3.Status != todo.StatusOpen {
		t.Errorf("status after 3 advances = %v, want open (wrapped)", t3.Status)
	}
}

func TestModel_Kanban_ColumnNavigation(t *testing.T) {
	m := newTestModel(t, layoutKanban)
	if m.column != 0 {
		t.Fatalf("initial column = %d, want 0", m.column)
	}
	m = send(t, m, "right")
	if m.column != 1 {
		t.Errorf("column after right = %d, want 1", m.column)
	}
	m = send(t, m, "left")
	if m.column != 0 {
		t.Errorf("column after left = %d, want 0", m.column)
	}
}

func TestModel_Kanban_MoveCardColumn(t *testing.T) {
	m := newTestModel(t, layoutKanban)
	task, ok := m.selectedTask()
	if !ok {
		t.Fatal("expected a selected task in open column")
	}

	m = send(t, m, "L")
	if m.column != 1 {
		t.Fatalf("column after L = %d, want 1 (in-progress)", m.column)
	}

	moved, err := m.svc.GetTask(m.ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if moved.Status != todo.StatusInProgress {
		t.Errorf("moved task status = %v, want in-progress", moved.Status)
	}
}

func TestModel_Quit(t *testing.T) {
	m := newTestModel(t, layoutList)
	next, cmd := m.Update(key("q"))
	m2 := next.(model)
	if !m2.quit {
		t.Fatalf("expected quit = true")
	}
	if cmd == nil {
		t.Fatalf("expected tea.Quit command")
	}
	if m2.View() != "" {
		t.Errorf("View() after quit should be empty")
	}
}

func TestView_AllLayoutsRenderTaskTitles(t *testing.T) {
	for _, layout := range []layoutKind{layoutList, layoutSplit, layoutKanban} {
		m := newTestModel(t, layout)
		m.width, m.height = 80, 24
		out := m.View()
		if !strings.Contains(out, "first task") {
			t.Errorf("layout %v: View() missing task title, got:\n%s", layout, out)
		}
	}
}

func TestParseLayout(t *testing.T) {
	cases := map[string]layoutKind{
		"list": layoutList, "split": layoutSplit, "kanban": layoutKanban, "": layoutList, "bogus": layoutList,
	}
	for in, want := range cases {
		if got := ParseLayout(in); got != want {
			t.Errorf("ParseLayout(%q) = %v, want %v", in, got, want)
		}
	}
}
