package tui

import (
	"fmt"
	"strings"

	"github.com/gbchd/todo/internal/service/todo"
)

// indent prefixes a Subtask's row so it reads as sitting under the Parent
// Task above it when subtasks are revealed.
func indent(t todo.Task) string {
	if !t.IsSubtask() {
		return ""
	}
	return "└ "
}

// rollup is a Parent Task's subtask progress, with a "!" when one of them is
// overdue — the marker that keeps an overdue subtask visible while subtasks
// are hidden.
func rollup(t todo.Task) string {
	if t.ChildCount == 0 {
		return ""
	}
	if t.AnyChildOverdue {
		return fmt.Sprintf(" (%d/%d !)", t.DoneChildCount, t.ChildCount)
	}
	return fmt.Sprintf(" (%d/%d)", t.DoneChildCount, t.ChildCount)
}

func dueString(t todo.Task) string {
	if t.DueDate == nil {
		return "-"
	}
	return t.DueDate.Format(todo.DateLayout)
}

// detailBody renders the field-per-line task detail shared by the modal
// (viewDetail) and the split layout's always-visible right pane
// (viewDetailPane); only the wrapper around it differs.
func detailBody(t todo.Task, children []todo.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n\n", t.ID, t.Title)
	fmt.Fprintf(&b, "Status:   %s %s\n", statusGlyph(string(t.Status)), t.Status)
	fmt.Fprintf(&b, "Priority: %s\n", priorityStyle(string(t.Priority)).Render(string(t.Priority)))
	fmt.Fprintf(&b, "Due:      %s\n", dueString(t))
	if t.IsSubtask() {
		fmt.Fprintf(&b, "Parent:   #%d\n", *t.ParentID)
	}
	b.WriteString("\n")
	if t.Description == "" {
		b.WriteString("(no description)\n")
	} else {
		b.WriteString(t.Description + "\n")
	}
	b.WriteString(subtaskSection(t, children))
	return b.String()
}

// subtaskSection lists a Parent Task's Subtasks. It is omitted entirely for a
// Subtask, which can never have children of its own.
func subtaskSection(t todo.Task, children []todo.Task) string {
	if t.IsSubtask() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nSubtasks (%d/%d)\n", t.DoneChildCount, t.ChildCount)
	if len(children) == 0 {
		b.WriteString(helpStyle.Render("(none yet)") + "\n")
	}
	for _, c := range children {
		fmt.Fprintf(&b, "  %s #%-3d %s  (due %s)\n", statusGlyph(string(c.Status)), c.ID, c.Title, dueString(c))
	}
	return b.String()
}

func viewDetail(t todo.Task, children []todo.Task, width int) string {
	help := "a: add subtask  e: edit  d: delete  esc: back"
	if t.IsSubtask() {
		help = "e: edit  d: delete  esc: back"
	}
	body := detailBody(t, children) + "\n" + helpStyle.Render(help)
	return modalStyle.Width(width).Render(body)
}

// viewDetailPane renders the same information without the modal border,
// for the split layout's always-visible right pane.
func viewDetailPane(t todo.Task, children []todo.Task) string {
	return detailBody(t, children)
}

func viewConfirm(id int64) string {
	text := fmt.Sprintf("Delete task #%d?\n\ny: yes   n/esc: cancel", id)
	return modalStyle.Render(text)
}
