package todo

import "context"

// TaskRepository is the secondary port the Service depends on. The SQLite
// adapter in internal/repository implements it; ISO-8601 parsing and NULL
// handling stay private to that adapter.
type TaskRepository interface {
	Create(ctx context.Context, t Task) (Task, error)
	Get(ctx context.Context, id int64) (Task, error)
	Update(ctx context.Context, t Task) (Task, error)
	// UpdateWith fetches the task with id, applies mutate to it, and persists
	// the result as a single atomic operation — implementations must run the
	// fetch and the write under one transaction (or equivalent lock) so a
	// concurrent UpdateWith/Update on the same id cannot interleave between
	// them and silently lose one of the two writes.
	UpdateWith(ctx context.Context, id int64, mutate func(Task) (Task, error)) (Task, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, filter TaskFilter) ([]Task, error)
}
