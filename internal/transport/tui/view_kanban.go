package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gbchd/todo/internal/service/todo"
)

var columnTitles = [3]string{"Open", "In Progress", "Done"}

const kanbanHelpLine = "←/→ column  ↑/↓ card  H/L move card  enter detail  a add  e edit  d delete  s subtasks  q quit"

func cardText(t todo.Task) string {
	due := dueString(t)
	line := fmt.Sprintf("#%d %s%s%s", t.ID, indent(t), t.Title, rollup(t))
	pri := priorityStyle(string(t.Priority)).Render(string(t.Priority))
	return line + "\n" + pri + "  due " + due
}

func (m model) viewKanbanBoard() string {
	cols := m.groupedByStatus()
	w, _ := m.size()
	colWidth := w/3 - 2

	selectedCol := m.kanbanColumn()
	var rendered [3]string
	for i, tasks := range cols {
		var b strings.Builder
		b.WriteString(columnHeaderStyle.Render(fmt.Sprintf("%s (%d)", columnTitles[i], len(tasks))) + "\n")
		selectedRow := -1
		if i == selectedCol {
			selectedRow = indexOfID(tasks, m.selectedID)
		}
		for j, t := range tasks {
			style := cardStyle
			if j == selectedRow {
				style = cardSelectedStyle
			}
			b.WriteString(style.Width(colWidth).Render(cardText(t)) + "\n")
		}
		rendered[i] = lipgloss.NewStyle().Width(colWidth + 2).Render(b.String())
	}

	board := lipgloss.JoinHorizontal(lipgloss.Top, rendered[0], rendered[1], rendered[2])
	return titleBarStyle.Render("todo — kanban") + "\n\n" + board + "\n\n" + helpStyle.Render(kanbanHelpLine)
}

func (m model) viewKanban() string {
	return m.overlay(m.viewKanbanBoard())
}
