package host

import (
	"time"

	"github.com/gbchd/todo/internal/service/todo"
)

// taskDTO is the host API's wire representation of a Task.
//
// Version is what makes a write conditional: a client that read a task hands
// the version it saw back as expected_version on the next PATCH. Like the
// derived Subtask rollup fields it is storage's to set, so both are read-only
// on the wire — see readOnlyFields.
type taskDTO struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	DueDate     *string `json:"due_date"`
	ParentID    *int64  `json:"parent_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	CompletedAt *string `json:"completed_at"`

	Version int64 `json:"version"`

	// Derived, read-only: a Parent Task's rolled-up view of its Subtasks.
	ChildCount      int  `json:"child_count"`
	DoneChildCount  int  `json:"done_child_count"`
	AnyChildOverdue bool `json:"any_child_overdue"`
}

func toDTO(t todo.Task) taskDTO {
	var due, completed *string
	if t.DueDate != nil {
		s := t.DueDate.Format(todo.DateLayout)
		due = &s
	}
	if t.CompletedAt != nil {
		s := t.CompletedAt.UTC().Format(time.RFC3339)
		completed = &s
	}
	return taskDTO{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		Priority:    string(t.Priority),
		DueDate:     due,
		ParentID:    t.ParentID,
		CreatedAt:   t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt.UTC().Format(time.RFC3339),
		CompletedAt: completed,

		Version: t.Version,

		ChildCount:      t.ChildCount,
		DoneChildCount:  t.DoneChildCount,
		AnyChildOverdue: t.AnyChildOverdue,
	}
}

func toDTOs(tasks []todo.Task) []taskDTO {
	out := make([]taskDTO, len(tasks))
	for i, t := range tasks {
		out[i] = toDTO(t)
	}
	return out
}

// createRequest is the POST /api/v1/tasks body: a deliberately narrower struct
// than taskDTO, so the fields storage owns have nowhere to arrive. A non-null
// parent_id creates the task as a Subtask of that task.
type createRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	DueDate     *string `json:"due_date"`
	ParentID    *int64  `json:"parent_id"`
}

// errorBody is the JSON shape of every error the host returns. Error is the
// message a human reads; the other fields exist so a client's HTTP repository
// adapter can rebuild the domain error this stands for by reading fields
// rather than parsing prose, and so satisfy the port's promise that callers
// cannot tell which adapter raised an error.
type errorBody struct {
	Error string `json:"error"`

	// Field and Message reproduce a *todo.ValidationError.
	Field   string `json:"field,omitempty"`
	Message string `json:"message,omitempty"`

	// Conflict reproduces a *todo.ConflictError. It is a nested object rather
	// than three sibling fields because version 0 is a legal value, so
	// "absent" cannot be encoded as "zero".
	Conflict *conflictBody `json:"conflict,omitempty"`
}

type conflictBody struct {
	TaskID   int64 `json:"task_id"`
	Expected int64 `json:"expected_version"`
	Actual   int64 `json:"actual_version"`
}
