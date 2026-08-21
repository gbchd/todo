// Package tui implements the `todo tui` interactive terminal adapter,
// shipping three interchangeable layouts (list/split/kanban) over the
// shared core Service.
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/transport/statusverb"
)

type layoutKind int

const (
	layoutList layoutKind = iota
	layoutSplit
	layoutKanban
)

// ParseLayout maps a --layout/tui_layout string to a layoutKind, defaulting
// to layoutList for anything unrecognized.
func ParseLayout(s string) layoutKind {
	switch s {
	case "split":
		return layoutSplit
	case "kanban":
		return layoutKanban
	default:
		return layoutList
	}
}

type mode int

const (
	modeBrowse mode = iota
	modeDetail
	modeForm
	modeConfirmDelete
)

var columnStatuses = [3]todo.Status{todo.StatusOpen, todo.StatusInProgress, todo.StatusDone}

// model is the single bubbletea model shared by all three layouts; layout
// controls rendering and a few movement rules (View, moveCursor/moveColumn).
type model struct {
	ctx context.Context
	svc *todo.Service

	layout layoutKind
	mode   mode

	tasks  []todo.Task
	cursor int
	column int // kanban only

	// showSubtasks is a viewing mode, not a preference: it resets on every
	// launch rather than being persisted to config.
	showSubtasks   bool
	detailChildren []todo.Task

	form            form
	pendingDeleteID int64

	err error

	width, height int
	quit          bool
}

func newModel(ctx context.Context, svc *todo.Service, layout layoutKind) model {
	m := model{ctx: ctx, svc: svc, layout: layout}
	m.reload()
	return m
}

func (m model) Init() tea.Cmd { return nil }

// reload re-fetches tasks and, for list/split layouts, keeps the cursor on
// the same task by id (the default sort reorders by status group, so a
// status-changing action can otherwise move a fixed index onto a different
// task). Kanban callers that move a card across columns fix up
// column/cursor themselves afterward.
func (m *model) reload() {
	var prevID int64
	hadSelection := false
	if m.layout != layoutKanban {
		if t, ok := m.selectedTask(); ok {
			prevID, hadSelection = t.ID, true
		}
	}

	filter := todo.TaskFilter{}
	if !m.showSubtasks {
		filter.ParentID = todo.Set[*int64](nil)
	}
	tasks, err := m.svc.ListTasks(m.ctx, filter)
	if err != nil {
		m.err = err
		return
	}
	m.err = nil
	m.tasks = tasks

	m.cursor = clamp(m.cursor, m.visibleCount()-1)
	if hadSelection {
		for i, t := range m.tasks {
			if t.ID == prevID {
				m.cursor = i
				break
			}
		}
	}
	m.loadDetailChildren()
}

// loadDetailChildren refreshes the Subtask list the detail view shows. It uses
// the same ParentID call path as `todo list --parent 4` rather than a dedicated
// TaskWithChildren type.
//
// It has to follow the cursor, not just reload/enter: the split layout renders
// the detail pane on every browse frame, so a stale list would show one task's
// subtasks under another task's heading.
func (m *model) loadDetailChildren() {
	t, ok := m.selectedTask()
	if !ok {
		m.detailChildren = nil
		return
	}
	children, err := m.svc.ListTasks(m.ctx, todo.TaskFilter{ParentID: todo.Set(&t.ID)})
	if err != nil {
		m.err = err
		return
	}
	m.detailChildren = children
}

func (m model) size() (int, int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

func (m model) groupedByStatus() [3][]todo.Task {
	var cols [3][]todo.Task
	for _, t := range m.tasks {
		switch t.Status {
		case todo.StatusOpen:
			cols[0] = append(cols[0], t)
		case todo.StatusInProgress:
			cols[1] = append(cols[1], t)
		case todo.StatusDone:
			cols[2] = append(cols[2], t)
		}
	}
	return cols
}

func (m model) visibleCount() int {
	if m.layout == layoutKanban {
		return len(m.groupedByStatus()[m.column])
	}
	return len(m.tasks)
}

func (m model) selectedTask() (todo.Task, bool) {
	if m.layout == layoutKanban {
		col := m.groupedByStatus()[m.column]
		if m.cursor < 0 || m.cursor >= len(col) {
			return todo.Task{}, false
		}
		return col[m.cursor], true
	}
	if m.cursor < 0 || m.cursor >= len(m.tasks) {
		return todo.Task{}, false
	}
	return m.tasks[m.cursor], true
}

func clamp(v, hi int) int {
	if hi < 0 {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

func (m *model) moveCursor(delta int) {
	n := m.visibleCount()
	if n == 0 {
		m.cursor = 0
		return
	}
	m.cursor = clamp(m.cursor+delta, n-1)
	m.loadDetailChildren()
}

func (m *model) moveColumn(delta int) {
	m.column = clamp(m.column+delta, 2)
	n := len(m.groupedByStatus()[m.column])
	m.cursor = clamp(m.cursor, n-1)
	m.loadDetailChildren()
}

func nextStatus(s todo.Status) todo.Status {
	switch s {
	case todo.StatusOpen:
		return todo.StatusInProgress
	case todo.StatusInProgress:
		return todo.StatusDone
	default:
		return todo.StatusOpen
	}
}

func columnIndex(s todo.Status) int {
	for i, cs := range columnStatuses {
		if cs == s {
			return i
		}
	}
	return 0
}

func (m *model) advanceStatus(t todo.Task) {
	next := nextStatus(t.Status)
	if _, err := statusverb.Apply(m.ctx, m.svc, t.ID, next); err != nil {
		m.err = err
		return
	}
	m.reload()
	if m.layout == layoutKanban {
		m.column = columnIndex(next)
		n := len(m.groupedByStatus()[m.column])
		m.cursor = clamp(m.cursor, n-1)
	}
}

func (m *model) moveCardColumn(delta int) {
	t, ok := m.selectedTask()
	if !ok {
		return
	}
	target := clamp(m.column+delta, 2)
	if target == m.column {
		return
	}
	if _, err := statusverb.Apply(m.ctx, m.svc, t.ID, columnStatuses[target]); err != nil {
		m.err = err
		return
	}
	m.reload()
	m.column = target
	n := len(m.groupedByStatus()[m.column])
	m.cursor = clamp(m.cursor, n-1)
}

func (m model) View() string {
	if m.quit {
		return ""
	}
	switch m.layout {
	case layoutSplit:
		return m.viewSplit()
	case layoutKanban:
		return m.viewKanban()
	default:
		return m.viewList()
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		return m.handleFormKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmDeleteKey(msg)
	case modeDetail:
		return m.handleDetailKey(msg)
	default:
		return m.handleBrowseKey(msg)
	}
}

func (m model) handleBrowseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quit = true
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "left", "h":
		if m.layout == layoutKanban {
			m.moveColumn(-1)
		}
	case "right", "l":
		if m.layout == layoutKanban {
			m.moveColumn(1)
		}
	case "H":
		if m.layout == layoutKanban {
			m.moveCardColumn(-1)
		}
	case "L":
		if m.layout == layoutKanban {
			m.moveCardColumn(1)
		}
	case "enter":
		if _, ok := m.selectedTask(); ok {
			m.mode = modeDetail
			m.loadDetailChildren()
		}
	case "s":
		m.showSubtasks = !m.showSubtasks
		m.reload()
	case "a":
		m.form = formForAdd()
		m.mode = modeForm
	case "e":
		if t, ok := m.selectedTask(); ok {
			m.form = formForEdit(t)
			m.mode = modeForm
		}
	case "d":
		if t, ok := m.selectedTask(); ok {
			m.pendingDeleteID = t.ID
			m.mode = modeConfirmDelete
		}
	case " ", "c":
		if t, ok := m.selectedTask(); ok {
			m.advanceStatus(t)
		}
	}
	return m, nil
}

func (m model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "esc", "enter", "q":
		m.mode = modeBrowse
	case "a":
		// Subtasks are only ever created from inside their parent, so "a"
		// still means plain "add" everywhere else and the parent is implicit.
		if t, ok := m.selectedTask(); ok && !t.IsSubtask() {
			m.form = formForAddSubtask(t.ID)
			m.mode = modeForm
		}
	case "e":
		if t, ok := m.selectedTask(); ok {
			m.form = formForEdit(t)
			m.mode = modeForm
		}
	case "d":
		if t, ok := m.selectedTask(); ok {
			m.pendingDeleteID = t.ID
			m.mode = modeConfirmDelete
		}
	}
	return m, nil
}

func (m model) handleConfirmDeleteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "y":
		if err := m.svc.DeleteTask(m.ctx, m.pendingDeleteID); err != nil {
			m.err = err
		}
		m.pendingDeleteID = 0
		m.mode = modeBrowse
		m.reload()
	case "n", "esc":
		m.pendingDeleteID = 0
		m.mode = modeBrowse
	}
	return m, nil
}

func (m model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "esc":
		m.mode = modeBrowse
		return m, nil
	case "tab":
		m.form = m.form.next()
		return m, nil
	case "shift+tab":
		m.form = m.form.prev()
		return m, nil
	case "left":
		if m.form.focus == fieldPriority {
			m.form = m.form.cyclePriority(-1)
			return m, nil
		}
	case "right":
		if m.form.focus == fieldPriority {
			m.form = m.form.cyclePriority(1)
			return m, nil
		}
	case "enter":
		return m.submitForm()
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.update(msg)
	return m, cmd
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.form.title.Value())
	if title == "" {
		m.form.err = "title must not be empty"
		return m, nil
	}
	due, err := m.form.parseDue()
	if err != nil {
		m.form.err = err.Error()
		return m, nil
	}

	if m.form.editingID == 0 {
		_, err = m.svc.AddTask(m.ctx, todo.NewTask{
			Title:       title,
			Description: m.form.description.Value(),
			Priority:    m.form.priority,
			DueDate:     due,
			ParentID:    m.form.parentID,
		})
	} else {
		_, err = m.svc.UpdateTask(m.ctx, m.form.editingID, todo.TaskPatch{
			Title:       todo.Set(title),
			Description: todo.Set(m.form.description.Value()),
			Priority:    todo.Set(m.form.priority),
			DueDate:     todo.Set(due),
		})
	}
	if err != nil {
		m.form.err = err.Error()
		return m, nil
	}
	// A subtask was added from its parent's detail view; go back to it so the
	// new subtask shows up where it was created.
	m.mode = modeBrowse
	if m.form.parentID != nil {
		m.mode = modeDetail
	}
	m.reload()
	return m, nil
}
