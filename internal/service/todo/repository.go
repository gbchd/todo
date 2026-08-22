package todo

import "context"

// TaskRepository is the secondary port the Service depends on. The SQLite
// adapter in internal/repository implements it; ISO-8601 parsing and NULL
// handling stay private to that adapter.
//
// Task.Version belongs to the implementation, not the caller: Create stores a
// task at version 1 and every subsequent write increments it, whatever version
// the Task handed in carries. It is the value a caller passes back as
// TaskPatch.ExpectedVersion to make a write conditional on nobody else having
// written since.
type TaskRepository interface {
	Create(ctx context.Context, t Task) (Task, error)
	Get(ctx context.Context, id int64) (Task, error)
	// UpdateWith fetches the task with id, applies mutate to it, and persists
	// the result as a single atomic operation — implementations must run the
	// fetch and the write under one transaction (or equivalent lock) so a
	// concurrent UpdateWith/Update on the same id cannot interleave between
	// them and silently lose one of the two writes.
	UpdateWith(ctx context.Context, id int64, mutate func(Task) (Task, error)) (Task, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, filter TaskFilter) ([]Task, error)
}
