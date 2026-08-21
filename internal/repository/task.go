package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gbchd/todo/internal/service/todo"
)

const (
	timestampLayout = time.RFC3339

	// selectTasks is the only way a task row is read. The self-join rolls up
	// each task's Subtasks into the derived ChildCount/DoneChildCount/
	// AnyChildOverdue fields, so those are populated identically by Get and
	// List rather than being real on one path and zero on the other.
	//
	// date('now','localtime') is the machine's calendar day, which is what a
	// day-granularity due date on a single-machine app means; comparing
	// against UTC would call a task overdue a few hours early or late.
	selectTasks = `SELECT t.id, t.title, t.description, t.status, t.priority, t.due_date, t.parent_id,
	       t.created_at, t.updated_at, t.completed_at,
	       COUNT(c.id),
	       COALESCE(SUM(c.status = 'done'), 0),
	       COALESCE(MAX(c.status <> 'done' AND c.due_date IS NOT NULL AND c.due_date < date('now','localtime')), 0)
	  FROM tasks t
	  LEFT JOIN tasks c ON c.parent_id = t.id`
)

func formatTimestamp(t time.Time) string { return t.UTC().Format(timestampLayout) }
func formatDate(t time.Time) string      { return t.UTC().Format(todo.DateLayout) }

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullTimestamp(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTimestamp(*t), Valid: true}
}

func nullDate(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatDate(*t), Valid: true}
}

func nullID(id *int64) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *id, Valid: true}
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// queryExecer is satisfied by both *sql.DB and *sql.Tx, letting getTask and
// updateTask run either directly against the pool or inside a transaction.
type queryExecer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func scanTask(row scanner) (todo.Task, error) {
	var (
		t                                 todo.Task
		description, dueDate, completedAt sql.NullString
		parentID                          sql.NullInt64
		status, priority                  string
		createdAt, updatedAt              string
		anyChildOverdue                   int
	)
	if err := row.Scan(&t.ID, &t.Title, &description, &status, &priority, &dueDate, &parentID,
		&createdAt, &updatedAt, &completedAt,
		&t.ChildCount, &t.DoneChildCount, &anyChildOverdue); err != nil {
		return todo.Task{}, err
	}

	t.Description = description.String
	t.Status = todo.Status(status)
	t.Priority = todo.Priority(priority)
	t.AnyChildOverdue = anyChildOverdue != 0

	if parentID.Valid {
		id := parentID.Int64
		t.ParentID = &id
	}

	if dueDate.Valid {
		d, err := time.Parse(todo.DateLayout, dueDate.String)
		if err != nil {
			return todo.Task{}, fmt.Errorf("parse due_date: %w", err)
		}
		t.DueDate = &d
	}

	created, err := time.Parse(timestampLayout, createdAt)
	if err != nil {
		return todo.Task{}, fmt.Errorf("parse created_at: %w", err)
	}
	t.CreatedAt = created

	updated, err := time.Parse(timestampLayout, updatedAt)
	if err != nil {
		return todo.Task{}, fmt.Errorf("parse updated_at: %w", err)
	}
	t.UpdatedAt = updated

	if completedAt.Valid {
		c, err := time.Parse(timestampLayout, completedAt.String)
		if err != nil {
			return todo.Task{}, fmt.Errorf("parse completed_at: %w", err)
		}
		t.CompletedAt = &c
	}

	return t, nil
}

// Create inserts t and returns the stored row (with id assigned).
func (r *SQLiteRepository) Create(ctx context.Context, t todo.Task) (todo.Task, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tasks (title, description, status, priority, due_date, parent_id, created_at, updated_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Title, nullString(t.Description), string(t.Status), string(t.Priority),
		nullDate(t.DueDate), nullID(t.ParentID),
		formatTimestamp(t.CreatedAt), formatTimestamp(t.UpdatedAt), nullTimestamp(t.CompletedAt),
	)
	if err != nil {
		return todo.Task{}, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return todo.Task{}, fmt.Errorf("last insert id: %w", err)
	}
	return r.Get(ctx, id)
}

// Get returns the task with the given id, or todo.ErrNotFound.
func (r *SQLiteRepository) Get(ctx context.Context, id int64) (todo.Task, error) {
	return getTask(ctx, r.db, id)
}

func getTask(ctx context.Context, q queryExecer, id int64) (todo.Task, error) {
	row := q.QueryRowContext(ctx, selectTasks+" WHERE t.id = ? GROUP BY t.id", id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return todo.Task{}, todo.ErrNotFound
	}
	if err != nil {
		return todo.Task{}, fmt.Errorf("get task %d: %w", id, err)
	}
	return t, nil
}

// updateTask overwrites the row matching t.ID with t's fields.
func updateTask(ctx context.Context, q queryExecer, t todo.Task) (todo.Task, error) {
	res, err := q.ExecContext(ctx,
		`UPDATE tasks SET title=?, description=?, status=?, priority=?, due_date=?, parent_id=?,
		        created_at=?, updated_at=?, completed_at=?
		 WHERE id=?`,
		t.Title, nullString(t.Description), string(t.Status), string(t.Priority),
		nullDate(t.DueDate), nullID(t.ParentID),
		formatTimestamp(t.CreatedAt), formatTimestamp(t.UpdatedAt), nullTimestamp(t.CompletedAt),
		t.ID,
	)
	if err != nil {
		return todo.Task{}, fmt.Errorf("update task %d: %w", t.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return todo.Task{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return todo.Task{}, todo.ErrNotFound
	}
	return getTask(ctx, q, t.ID)
}

// UpdateWith fetches the task with id, applies mutate, and writes the result
// back inside a single transaction. Service methods use this (rather than a
// separate Get + Update) so a concurrent PATCH/status-verb call on the same
// task can't read the same "existing" row and then overwrite this call's
// write with its own — the transaction serializes the two read-modify-write
// cycles instead of letting them interleave.
func (r *SQLiteRepository) UpdateWith(ctx context.Context, id int64, mutate func(todo.Task) (todo.Task, error)) (todo.Task, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return todo.Task{}, fmt.Errorf("begin update tx for task %d: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once Commit succeeds

	existing, err := getTask(ctx, tx, id)
	if err != nil {
		return todo.Task{}, err
	}

	updated, err := mutate(existing)
	if err != nil {
		return todo.Task{}, err
	}

	t, err := updateTask(ctx, tx, updated)
	if err != nil {
		return todo.Task{}, err
	}

	if err := tx.Commit(); err != nil {
		return todo.Task{}, fmt.Errorf("commit update tx for task %d: %w", id, err)
	}
	return t, nil
}

// Delete removes the task with the given id, or returns todo.ErrNotFound.
func (r *SQLiteRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete task %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return todo.ErrNotFound
	}
	return nil
}

// List returns tasks matching filter, ordered by id (Service applies the
// final user-facing sort).
func (r *SQLiteRepository) List(ctx context.Context, filter todo.TaskFilter) ([]todo.Task, error) {
	query := selectTasks + " WHERE 1=1"
	var args []any

	if filter.Status != nil {
		query += " AND t.status = ?"
		args = append(args, string(*filter.Status))
	}
	if filter.Priority != nil {
		query += " AND t.priority = ?"
		args = append(args, string(*filter.Priority))
	}
	if filter.DueBefore != nil {
		query += " AND t.due_date IS NOT NULL AND t.due_date < ?"
		args = append(args, formatDate(*filter.DueBefore))
	}
	if filter.DueAfter != nil {
		query += " AND t.due_date IS NOT NULL AND t.due_date > ?"
		args = append(args, formatDate(*filter.DueAfter))
	}
	if filter.ParentID.IsSet() {
		if parent := filter.ParentID.Value(); parent == nil {
			query += " AND t.parent_id IS NULL"
		} else {
			query += " AND t.parent_id = ?"
			args = append(args, *parent)
		}
	}
	query += " GROUP BY t.id ORDER BY t.id"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []todo.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, nil
}
