package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gbchd/todo/internal/service/todo"
)

// formField indexes the focusable fields of the add/edit form, in the order
// they mirror todo.TaskPatch: Title, Description, Priority, DueDate.
type formField int

const (
	fieldTitle formField = iota
	fieldDescription
	fieldPriority
	fieldDueDate
	fieldCount
)

var priorityCycle = []todo.Priority{todo.PriorityNone, todo.PriorityLow, todo.PriorityMedium, todo.PriorityHigh}

// form holds add/edit form state. editingID is 0 for "add".
type form struct {
	editingID   int64
	title       textinput.Model
	description textinput.Model
	dueDate     textinput.Model
	priority    todo.Priority
	focus       formField
	err         string
}

func newForm() form {
	title := textinput.New()
	title.Placeholder = "Title"
	title.Focus()
	title.CharLimit = 200

	desc := textinput.New()
	desc.Placeholder = "Description"
	desc.CharLimit = 2000

	due := textinput.New()
	due.Placeholder = "YYYY-MM-DD (blank for none)"
	due.CharLimit = 10

	return form{title: title, description: desc, dueDate: due, priority: todo.PriorityNone}
}

func formForAdd() form {
	return newForm()
}

func formForEdit(t todo.Task) form {
	f := newForm()
	f.editingID = t.ID
	f.title.SetValue(t.Title)
	f.description.SetValue(t.Description)
	f.priority = t.Priority
	if t.DueDate != nil {
		f.dueDate.SetValue(t.DueDate.Format(todo.DateLayout))
	}
	return f
}

func (f *form) setFocus(field formField) {
	f.focus = field
	f.title.Blur()
	f.description.Blur()
	f.dueDate.Blur()
	switch field {
	case fieldTitle:
		f.title.Focus()
	case fieldDescription:
		f.description.Focus()
	case fieldDueDate:
		f.dueDate.Focus()
	}
}

func (f form) next() form {
	f.setFocus((f.focus + 1) % fieldCount)
	return f
}

func (f form) prev() form {
	f.setFocus((f.focus - 1 + fieldCount) % fieldCount)
	return f
}

func (f form) cyclePriority(delta int) form {
	idx := 0
	for i, p := range priorityCycle {
		if p == f.priority {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(priorityCycle)) % len(priorityCycle)
	f.priority = priorityCycle[idx]
	return f
}

// parseDue parses the due-date field, "" meaning no due date.
func (f form) parseDue() (*time.Time, error) {
	s := strings.TrimSpace(f.dueDate.Value())
	if s == "" {
		return nil, nil
	}
	d, err := time.Parse(todo.DateLayout, s)
	if err != nil {
		return nil, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", s)
	}
	return &d, nil
}

// update routes a key/message to the focused field and returns the possibly
// mutated form and a command.
func (f form) update(msg tea.Msg) (form, tea.Cmd) {
	var cmd tea.Cmd
	switch f.focus {
	case fieldTitle:
		f.title, cmd = f.title.Update(msg)
	case fieldDescription:
		f.description, cmd = f.description.Update(msg)
	case fieldDueDate:
		f.dueDate, cmd = f.dueDate.Update(msg)
	}
	return f, cmd
}

func (f form) view(width int) string {
	var b strings.Builder
	label := func(i formField, text string) string {
		if f.focus == i {
			return selectedStyle.Render(text)
		}
		return text
	}

	fmt.Fprintf(&b, "%s\n%s\n\n", label(fieldTitle, "Title"), f.title.View())
	fmt.Fprintf(&b, "%s\n%s\n\n", label(fieldDescription, "Description"), f.description.View())
	fmt.Fprintf(&b, "%s\n%s (←/→ to change)\n\n", label(fieldPriority, "Priority"), priorityStyle(string(f.priority)).Render(string(f.priority)))
	fmt.Fprintf(&b, "%s\n%s\n\n", label(fieldDueDate, "Due date"), f.dueDate.View())

	if f.err != "" {
		b.WriteString(errorStyle.Render("Error: " + f.err))
		b.WriteString("\n\n")
	}
	b.WriteString(helpStyle.Render("tab/shift+tab: field  ←/→: priority  enter: save  esc: cancel"))
	return modalStyle.Width(width).Render(b.String())
}
