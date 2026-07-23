// Package statusverb maps a target lifecycle Status onto the Service verb
// that produces it (Start/Complete/Reopen). The shared core deliberately
// has no generic SetStatus method — "explicit verb methods rather than a
// generic SetStatus... no invalid status string can be constructed" — so
// any adapter that receives a status as data (a kanban drop, a wire PATCH
// body) needs this same mapping to reach the right verb. Centralized here
// so the TUI and web adapters, which both take a status off the wire/UI,
// share one copy instead of each re-deriving it.
package statusverb

import (
	"context"

	"github.com/gbchd/todo/internal/service/todo"
)

// Apply invokes the Service verb that transitions id to target.
func Apply(ctx context.Context, svc *todo.Service, id int64, target todo.Status) (todo.Task, error) {
	switch target {
	case todo.StatusOpen:
		return svc.ReopenTask(ctx, id)
	case todo.StatusInProgress:
		return svc.StartTask(ctx, id)
	case todo.StatusDone:
		return svc.CompleteTask(ctx, id)
	default:
		return todo.Task{}, &todo.ValidationError{Field: "status", Message: "invalid status " + string(target)}
	}
}
