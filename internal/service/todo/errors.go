package todo

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned (wrapped, check via errors.Is) when a task id
// doesn't exist.
var ErrNotFound = errors.New("task not found")

// ValidationError reports a rejected input field; check via errors.As.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ConflictError reports that a write was rejected because the task had moved
// on since the caller read it: the TaskPatch.ExpectedVersion the caller
// supplied no longer matches the stored Task.Version. Check via errors.As.
//
// It carries both versions because the only useful thing a caller can do is
// re-read and retry, and knowing how far behind it was is what tells a human
// whether that is safe.
type ConflictError struct {
	TaskID   int64
	Expected int64
	Actual   int64
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("task %d: expected version %d, but it is now %d", e.TaskID, e.Expected, e.Actual)
}
