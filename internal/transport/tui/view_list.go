package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gbchd/todo/internal/service/todo"
)

const helpLine = "↑/↓ nav  enter detail  a add  e edit  d delete  space advance  q quit"

func rowText(t todo.Task) string {
	due := dueString(t)
	return fmt.Sprintf("%s #%-3d %-8s %-4s  %s  (due %s)",
		statusGlyph(string(t.Status)), t.ID, t.Status, t.Priority, t.Title, due)
}

func viewTaskList(tasks []todo.Task, cursor int) string {
	var b strings.Builder
	b.WriteString(titleBarStyle.Render("todo — list") + "\n\n")

	if len(tasks) == 0 {
		b.WriteString("(no tasks)\n")
	}
	for i, t := range tasks {
		row := rowText(t)
		if i == cursor {
			row = selectedStyle.Render(row)
		}
		b.WriteString(row + "\n")
	}

	b.WriteString("\n" + helpStyle.Render(helpLine))
	return b.String()
}

func (m model) viewList() string {
	background := viewTaskList(m.tasks, m.cursor)
	w, h := m.size()

	switch m.mode {
	case modeForm:
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, m.form.view(min(w-4, 60)))
	case modeDetail:
		if t, ok := m.selectedTask(); ok {
			return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, viewDetail(t, min(w-4, 60)))
		}
	case modeConfirmDelete:
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, viewConfirm(m.pendingDeleteID))
	}
	return background
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
