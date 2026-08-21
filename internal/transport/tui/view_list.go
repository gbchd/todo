package tui

import (
	"fmt"
	"strings"

	"github.com/gbchd/todo/internal/service/todo"
)

const helpLine = "↑/↓ nav  enter detail  a add  e edit  d delete  space advance  s subtasks  q quit"

func rowText(t todo.Task) string {
	due := dueString(t)
	return fmt.Sprintf("%s #%-3d %-8s %-4s  %s%s%s  (due %s)",
		statusGlyph(string(t.Status)), t.ID, t.Status, t.Priority, indent(t), t.Title, rollup(t), due)
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
	background := viewTaskList(m.tasks, indexOfID(m.tasks, m.selectedID))
	return m.overlay(background)
}
