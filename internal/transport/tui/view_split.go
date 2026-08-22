package tui

import (
	"github.com/charmbracelet/lipgloss"
)

func (m model) viewSplit() string {
	w, _ := m.size()
	leftWidth := w * 2 / 5
	rightWidth := w - leftWidth - 3

	left := lipgloss.NewStyle().Width(leftWidth).Render(viewTaskList(m.tasks, indexOfID(m.tasks, m.selectedID)))

	var right string
	switch m.mode {
	case modeForm:
		right = m.form.view(rightWidth)
	case modeConfirmDelete:
		right = viewConfirm(m.pendingDeleteID)
	default:
		if t, ok := m.selectedTask(); ok {
			right = detailBody(t, m.detailChildren())
		} else {
			right = helpStyle.Render("No tasks")
		}
	}
	rightPane := lipgloss.NewStyle().Width(rightWidth).Padding(0, 1).Render(right)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, " │ ", rightPane)
}
