package todo

import "context"

// TaskRepository is the secondary port the Service depends on. The SQLite
// adapter in internal/repository implements it; ISO-8601 parsing and NULL
// handling stay private to that adapter.
type TaskRepository interface {
	Create(ctx context.Context, t Task) (Task, error)
	Get(ctx context.Context, id int64) (Task, error)
	Update(ctx context.Context, t Task) (Task, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, filter TaskFilter) ([]Task, error)
}
