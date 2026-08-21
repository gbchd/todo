package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/gbchd/todo/internal/service/todo"
)

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
)

func colorize(s, code string, enabled bool) string {
	if !enabled || code == "" {
		return s
	}
	return code + s + ansiReset
}

func statusColor(s todo.Status) string {
	switch s {
	case todo.StatusDone:
		return ansiDim + ansiGreen
	case todo.StatusInProgress:
		return ansiYellow
	default:
		return ""
	}
}

func priorityColor(p todo.Priority) string {
	switch p {
	case todo.PriorityHigh:
		return ansiRed
	case todo.PriorityMedium:
		return ansiYellow
	default:
		return ""
	}
}

// printTable renders tasks as an aligned ID | STATUS | PRI | TITLE | DUE
// table. color enables ANSI status/priority coloring (TTY stdout only).
func printTable(w io.Writer, tasks []todo.Task, color bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tPRI\tTITLE\tDUE")
	if len(tasks) == 0 {
		fmt.Fprintln(tw, "(no tasks)")
		tw.Flush()
		return
	}
	for _, t := range tasks {
		due := formatDate(t.DueDate)
		if due == "" {
			due = "-"
		}
		status := colorize(string(t.Status), statusColor(t.Status), color)
		priority := colorize(string(t.Priority), priorityColor(t.Priority), color)
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", t.ID, status, priority, titleCell(t), due)
	}
	tw.Flush()
}

// titleCell renders the TITLE column. A Subtask is indented beneath its
// Parent Task, and a Parent Task carries its Subtasks' rolled-up progress plus
// a "!" when one of them is overdue — without that marker, hiding subtasks by
// default would make an overdue subtask invisible.
func titleCell(t todo.Task) string {
	title := t.Title
	if t.IsSubtask() {
		title = "└ " + title
	}
	if t.ChildCount == 0 {
		return title
	}
	overdue := ""
	if t.AnyChildOverdue {
		overdue = " !"
	}
	return fmt.Sprintf("%s (%d/%d%s)", title, t.DoneChildCount, t.ChildCount, overdue)
}

// printDetail renders a single task as a labeled field-per-line view.
func printDetail(w io.Writer, t todo.Task, color bool) {
	fmt.Fprintf(w, "ID:          %d\n", t.ID)
	fmt.Fprintf(w, "Title:       %s\n", t.Title)
	fmt.Fprintf(w, "Status:      %s\n", colorize(string(t.Status), statusColor(t.Status), color))
	fmt.Fprintf(w, "Priority:    %s\n", colorize(string(t.Priority), priorityColor(t.Priority), color))
	due := formatDate(t.DueDate)
	if due == "" {
		due = "-"
	}
	fmt.Fprintf(w, "Due:         %s\n", due)
	if t.IsSubtask() {
		fmt.Fprintf(w, "Parent:      #%d\n", *t.ParentID)
	}
	if t.ChildCount > 0 {
		overdue := ""
		if t.AnyChildOverdue {
			overdue = " (one is overdue)"
		}
		fmt.Fprintf(w, "Subtasks:    %d/%d done%s\n", t.DoneChildCount, t.ChildCount, overdue)
	}
	fmt.Fprintf(w, "Created:     %s\n", t.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "Updated:     %s\n", t.UpdatedAt.Format("2006-01-02 15:04"))
	if t.CompletedAt != nil {
		fmt.Fprintf(w, "Completed:   %s\n", t.CompletedAt.Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(w, "Description:\n")
	if t.Description == "" {
		fmt.Fprintln(w, "  (none)")
	} else {
		fmt.Fprintf(w, "  %s\n", t.Description)
	}
}
