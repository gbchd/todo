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
//
// ParentID makes a Task a Subtask of another Task, one level deep — a Task
// with a parent may never itself have children. ChildCount, DoneChildCount
// and AnyChildOverdue are derived: repositories populate them on every read
// so a Parent Task's row can roll up its children's state, and ignore them
// on write. See docs/adr/0001-subtasks-as-self-referential-tasks.md.
type Task struct {
	ID          int64
	Title       string
	Description string
	Status      Status
	Priority    Priority
	DueDate     *time.Time
	ParentID    *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time

	ChildCount      int
	DoneChildCount  int
	AnyChildOverdue bool
}

// IsSubtask reports whether t has a Parent Task. Because nesting is one level
// deep, its negation is also "t may have Subtasks of its own".
func (t Task) IsSubtask() bool { return t.ParentID != nil }

// NewTask is the input to Service.AddTask.
type NewTask struct {
	Title       string
	Description string
	Priority    Priority
	DueDate     *time.Time
	ParentID    *int64
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
// A DueDate set to a nil *time.Time clears the due date; a ParentID set to a
// nil *int64 promotes a Subtask back to top level. Setting Status applies the
// same lifecycle transition as the explicit Start/Complete/Reopen verbs,
// atomically with the rest of the patch.
type TaskPatch struct {
	Title       Optional[string]
	Description Optional[string]
	Priority    Optional[Priority]
	DueDate     Optional[*time.Time]
	ParentID    Optional[*int64]
	Status      Optional[Status]
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
//
// ParentID is three-valued and so cannot be a plain *int64: unset matches any
// task, Set(nil) matches top-level tasks only, and Set(&id) matches the
// Subtasks of id.
type TaskFilter struct {
	Status    *Status
	Priority  *Priority
	DueBefore *time.Time
	DueAfter  *time.Time
	ParentID  Optional[*int64]
	SortBy    SortKey
}

func normalizeDate(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &d
}
