// Package todo holds the domain model and business logic shared by every
// interface (CLI, TUI, web) of the todo app.
package todo

import "time"

// DateLayout is the calendar-day format (no time component) used to parse
// and render DueDate wherever it crosses an adapter or storage boundary.
const DateLayout = "2006-01-02"

// Status is a task's lifecycle state.
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in-progress"
	StatusDone       Status = "done"
)

// Priority orders tasks; PriorityHigh sorts before PriorityNone.
type Priority string

const (
	PriorityNone   Priority = "none"
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

var priorityRank = map[Priority]int{
	PriorityHigh:   3,
	PriorityMedium: 2,
	PriorityLow:    1,
	PriorityNone:   0,
}

func validPriority(p Priority) bool {
	_, ok := priorityRank[p]
	return ok
}

func validStatus(s Status) bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusDone:
		return true
	}
	return false
}

// Task is the domain entity. Repositories map storage rows to and from Task;
// nothing outside internal/repository knows how a Task is persisted.
type Task struct {
	ID          int64
	Title       string
	Description string
	Status      Status
	Priority    Priority
	DueDate     *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
}

// NewTask is the input to Service.AddTask.
type NewTask struct {
	Title       string
	Description string
	Priority    Priority
	DueDate     *time.Time
}

// Optional distinguishes "untouched" from "explicitly set to this value"
// (including a zero value) for patch-style updates.
type Optional[T any] struct {
	set   bool
	value T
}

// Set returns an Optional carrying v, marked as explicitly provided.
func Set[T any](v T) Optional[T] {
	return Optional[T]{set: true, value: v}
}

// IsSet reports whether the field was explicitly provided.
func (o Optional[T]) IsSet() bool { return o.set }

// Value returns the provided value; meaningless when IsSet is false.
func (o Optional[T]) Value() T { return o.value }

// TaskPatch is a partial update: an unset field is left untouched.
// A DueDate set to a nil *time.Time clears the due date.
type TaskPatch struct {
	Title       Optional[string]
	Description Optional[string]
	Priority    Optional[Priority]
	DueDate     Optional[*time.Time]
}

// SortKey selects the ordering ListTasks applies.
type SortKey string

const (
	SortDefault  SortKey = ""
	SortPriority SortKey = "priority"
	SortID       SortKey = "id"
	SortCreated  SortKey = "created"
)

// TaskFilter narrows ListTasks. Nil fields are unconstrained.
type TaskFilter struct {
	Status    *Status
	Priority  *Priority
	DueBefore *time.Time
	DueAfter  *time.Time
	SortBy    SortKey
}

func normalizeDate(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &d
}
