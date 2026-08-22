package todo

import "context"

// TaskRepository is the secondary port the Service depends on. Two adapters
// implement it: the SQLite one in internal/repository, which owns a database
// file on this machine, and the HTTP one in internal/repository/remote, which
// reads and writes the same tasks through a `todo host`. ISO-8601 parsing and
// NULL handling stay private to the first; JSON and status codes stay private
// to the second.
//
// Task.Version belongs to the implementation, not the caller: Create stores a
// task at version 1 and every subsequent write increments it, whatever version
// the Task handed in carries. It is the value a caller passes back as
// TaskPatch.ExpectedVersion to make a write conditional on nobody else having
// written since.
//
// # Timestamps are storage's, not the caller's
//
// CreatedAt, UpdatedAt and CompletedAt are stamped by whichever machine owns
// the tasks, so a Task that comes back from a write is not guaranteed to carry
// the timestamps the Task handed in did. The SQLite adapter stores what it is
// given and so returns it unchanged; the HTTP adapter is talking to a host
// whose clock is authoritative — it re-derives all three and discards what the
// client sent, which is what stops a device with a skewed clock from creating
// a task that sorts wrongly on every other device forever. Callers may read
// these fields; they may not assume a write echoed theirs back.
//
// # Atomicity
//
// UpdateWith is a read-modify-write that no concurrent write may interleave
// with. Each adapter satisfies that differently, and both are required to:
//
//   - SQLite: the fetch, the mutation and the write run inside one
//     transaction, on a pool pinned to a single connection.
//   - HTTP: the closure cannot be sent over the wire, so the adapter fetches,
//     applies the mutation locally, and sends the result guarded by a
//     precondition on the version it read. A write that landed in between
//     fails that precondition, and the adapter re-fetches and re-runs the
//     mutation a bounded number of times before surfacing *ConflictError. The
//     retry is safe because the mutation is a pure function of the task it is
//     given — which is why it must stay one.
type TaskRepository interface {
	Create(ctx context.Context, t Task) (Task, error)
	Get(ctx context.Context, id int64) (Task, error)
	// UpdateWith fetches the task with id, applies mutate to it, and persists
	// the result as a single atomic operation: a concurrent UpdateWith/Update
	// on the same id cannot interleave between the fetch and the write and
	// silently lose one of the two writes. See "Atomicity" above for how each
	// adapter delivers that.
	//
	// mutate must be a pure function of the task it is handed: it may be
	// called more than once, against a freshly read task each time.
	UpdateWith(ctx context.Context, id int64, mutate func(Task) (Task, error)) (Task, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, filter TaskFilter) ([]Task, error)
}
