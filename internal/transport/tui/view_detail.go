package tui

import (
	"fmt"
	"strings"

	"github.com/gbchd/todo/internal/service/todo"
)

func dueString(t todo.Task) string {
	if t.DueDate == nil {
		return "-"
	}
	return t.DueDate.Format(todo.DateLayout)
}

// detailBody renders the field-per-line task detail shared by the modal
// (viewDetail) and the split layout's always-visible right pane
// (viewDetailPane); only the wrapper around it differs.
func detailBody(t todo.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n\n", t.ID, t.Title)
	fmt.Fprintf(&b, "Status:   %s %s\n", statusGlyph(string(t.Status)), t.Status)
	fmt.Fprintf(&b, "Priority: %s\n", priorityStyle(string(t.Priority)).Render(string(t.Priority)))
	fmt.Fprintf(&b, "Due:      %s\n\n", dueString(t))
	if t.Description == "" {
		b.WriteString("(no description)\n")
	} else {
		b.WriteString(t.Description + "\n")
	}
	return b.String()
}

func viewDetail(t todo.Task, width int) string {
	body := detailBody(t) + "\n" + helpStyle.Render("e: edit  d: delete  esc: back")
	return modalStyle.Width(width).Render(body)
}

// viewDetailPane renders the same information without the modal border,
// for the split layout's always-visible right pane.
func viewDetailPane(t todo.Task) string {
	return detailBody(t)
}

func viewConfirm(id int64) string {
	text := fmt.Sprintf("Delete task #%d?\n\ny: yes   n/esc: cancel", id)
	return modalStyle.Render(text)
}
