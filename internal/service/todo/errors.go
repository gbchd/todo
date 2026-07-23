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
