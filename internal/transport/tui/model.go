// Package tui implements the `todo tui` interactive terminal adapter,
// shipping three interchangeable layouts (list/split/kanban) over the
// shared core Service.
package tui

import (
	"context"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gbchd/todo/internal/service/todo"
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

	tasks []todo.Task

	// selectedID is the source of truth for what's selected — an identity,
	// not a position. A reload or a status change can reorder or regroup
	// tasks; deriving the render position from this id (selectedTask,
	// kanbanColumn) instead of caching an index keeps the selection glued to
	// the task even when its position moves.
	selectedID int64

	// focusColumn is the kanban column with input focus when selectedID
	// names no task there — e.g. an empty column, or an empty board. It has
	// no effect on list/split, where selection alone drives the cursor.
	focusColumn int

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

// reload re-fetches tasks and keeps the selection on the same task by id (the
// default sort reorders by status group, so a status-changing action can
// otherwise move a fixed position onto a different task). If that task is
// gone — deleted, or filtered out by the subtask toggle — it falls back to
// whatever now sits nearest its old position, rather than jumping to the top.
func (m *model) reload() {
	fallbackCol := m.kanbanColumn()
	var fallbackIdx int
	if m.layout == layoutKanban {
		fallbackIdx = indexOfID(m.groupedByStatus()[fallbackCol], m.selectedID)
	} else {
		fallbackIdx = indexOfID(m.tasks, m.selectedID)
	}
	fallbackIdx = max(fallbackIdx, 0)

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

	if _, ok := m.selectedTask(); ok {
		m.setSelection(m.selectedID) // id unchanged; still refresh detailChildren
		return
	}
	if m.layout == layoutKanban {
		m.focusColumn = fallbackCol
		m.selectNearest(m.groupedByStatus()[fallbackCol], fallbackIdx)
		return
	}
	m.selectNearest(m.tasks, fallbackIdx)
}

// loadDetailChildren refreshes the Subtask list the detail view shows. It uses
// the same ParentID call path as `todo list --parent 4` rather than a dedicated
// TaskWithChildren type.
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

// setSelection is the one place a selection change funnels through, so
// detailChildren can never drift out of sync with what's selected — every
// caller that changes selectedID goes through here (or selectNearest, which
// calls it) instead of assigning the field directly.
func (m *model) setSelection(id int64) {
	m.selectedID = id
	m.loadDetailChildren()
}

// selectNearest selects the task at idx in ts, clamped, or clears the
// selection if ts is empty. It's the shared "land somewhere sensible"
// fallback for a cursor move and for a reload whose previous selection
// disappeared.
func (m *model) selectNearest(ts []todo.Task, idx int) {
	if len(ts) == 0 {
		m.setSelection(0)
		return
	}
	m.setSelection(ts[clamp(idx, len(ts)-1)].ID)
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

func (m model) selectedTask() (todo.Task, bool) {
	idx := indexOfID(m.tasks, m.selectedID)
	if idx < 0 {
		return todo.Task{}, false
	}
	return m.tasks[idx], true
}

// kanbanColumn is the column the selection belongs to, derived from the
// selected task's status. It falls back to focusColumn when nothing is
// selected there, e.g. an empty column or an empty board.
func (m model) kanbanColumn() int {
	if t, ok := m.selectedTask(); ok {
		return columnIndex(t.Status)
	}
	return m.focusColumn
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

// indexOfID returns id's position in tasks, or -1.
func indexOfID(tasks []todo.Task, id int64) int {
	return slices.IndexFunc(tasks, func(t todo.Task) bool { return t.ID == id })
}

func (m *model) moveCursor(delta int) {
	ts := m.tasks
	if m.layout == layoutKanban {
		ts = m.groupedByStatus()[m.kanbanColumn()]
	}
	m.selectNearest(ts, max(indexOfID(ts, m.selectedID), 0)+delta)
}

// moveColumn changes which kanban column has focus (left/right), preserving
// the selection's row position in the new column where possible. It does not
// move a card — see moveCardColumn for that (H/L).
func (m *model) moveColumn(delta int) {
	col := m.kanbanColumn()
	row := max(indexOfID(m.groupedByStatus()[col], m.selectedID), 0)
	target := clamp(col+delta, 2)
	m.focusColumn = target
	m.selectNearest(m.groupedByStatus()[target], row)
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
	if i := slices.Index(columnStatuses[:], s); i >= 0 {
		return i
	}
	return 0
}

// advanceStatus cycles t to its next Status. reload() alone keeps the
// selection on t: its id doesn't change, so once the reload sees t is still
// present (just regrouped into its new column), selectedTask finds it again.
func (m *model) advanceStatus(t todo.Task) {
	next := nextStatus(t.Status)
	if _, err := m.svc.UpdateTask(m.ctx, t.ID, todo.TaskPatch{Status: todo.Set(next)}); err != nil {
		m.err = err
		return
	}
	m.reload()
}

// moveCardColumn moves the selected card to the adjacent kanban column
// (H/L), changing its Status. Like advanceStatus, reload() alone keeps the
// selection on the moved card.
func (m *model) moveCardColumn(delta int) {
	t, ok := m.selectedTask()
	if !ok {
		return
	}
	col := m.kanbanColumn()
	target := clamp(col+delta, 2)
	if target == col {
		return
	}
	if _, err := m.svc.UpdateTask(m.ctx, t.ID, todo.TaskPatch{Status: todo.Set(columnStatuses[target])}); err != nil {
		m.err = err
		return
	}
	m.reload()
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

// overlay renders whatever modal is active (form, detail, or delete
// confirmation) on top of background, or returns background unchanged in
// modeBrowse. Shared by the list and kanban layouts, whose full-screen modal
// is identical; the split layout renders its detail pane inline instead, so
// it doesn't use this.
func (m model) overlay(background string) string {
	w, h := m.size()
	switch m.mode {
	case modeForm:
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, m.form.view(min(w-4, 60)))
	case modeDetail:
		if t, ok := m.selectedTask(); ok {
			return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, viewDetail(t, m.detailChildren, min(w-4, 60)))
		}
	case modeConfirmDelete:
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, viewConfirm(m.pendingDeleteID))
	case modeBrowse:
	}
	return background
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
