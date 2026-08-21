package web

import (
	"time"

	"github.com/gbchd/todo/internal/service/todo"
)

// taskDTO is the JSON wire representation of a task.
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

// createRequest is the POST /api/tasks request body. A non-null parent_id
// creates the task as a Subtask of that task.
type createRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	DueDate     *string `json:"due_date"`
	ParentID    *int64  `json:"parent_id"`
}
