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
	dateLayout      = "2006-01-02"

	taskColumns = "id, title, description, status, priority, due_date, created_at, updated_at, completed_at"
)

func formatTimestamp(t time.Time) string { return t.UTC().Format(timestampLayout) }
func formatDate(t time.Time) string      { return t.UTC().Format(dateLayout) }

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

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (todo.Task, error) {
	var (
		t                                 todo.Task
		description, dueDate, completedAt sql.NullString
		status, priority                  string
		createdAt, updatedAt              string
	)
	if err := row.Scan(&t.ID, &t.Title, &description, &status, &priority, &dueDate, &createdAt, &updatedAt, &completedAt); err != nil {
		return todo.Task{}, err
	}

	t.Description = description.String
	t.Status = todo.Status(status)
	t.Priority = todo.Priority(priority)

	if dueDate.Valid {
		d, err := time.Parse(dateLayout, dueDate.String)
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
		`INSERT INTO tasks (title, description, status, priority, due_date, created_at, updated_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Title, nullString(t.Description), string(t.Status), string(t.Priority),
		nullDate(t.DueDate), formatTimestamp(t.CreatedAt), formatTimestamp(t.UpdatedAt), nullTimestamp(t.CompletedAt),
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
	row := r.db.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = ?", id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return todo.Task{}, todo.ErrNotFound
	}
	if err != nil {
		return todo.Task{}, fmt.Errorf("get task %d: %w", id, err)
	}
	return t, nil
}

// Update overwrites the row matching t.ID with t's fields.
func (r *SQLiteRepository) Update(ctx context.Context, t todo.Task) (todo.Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET title=?, description=?, status=?, priority=?, due_date=?, created_at=?, updated_at=?, completed_at=?
		 WHERE id=?`,
		t.Title, nullString(t.Description), string(t.Status), string(t.Priority),
		nullDate(t.DueDate), formatTimestamp(t.CreatedAt), formatTimestamp(t.UpdatedAt), nullTimestamp(t.CompletedAt),
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
	return r.Get(ctx, t.ID)
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
	query := "SELECT " + taskColumns + " FROM tasks WHERE 1=1"
	var args []any

	if filter.Status != nil {
		query += " AND status = ?"
		args = append(args, string(*filter.Status))
	}
	if filter.Priority != nil {
		query += " AND priority = ?"
		args = append(args, string(*filter.Priority))
	}
	if filter.DueBefore != nil {
		query += " AND due_date IS NOT NULL AND due_date < ?"
		args = append(args, formatDate(*filter.DueBefore))
	}
	if filter.DueAfter != nil {
		query += " AND due_date IS NOT NULL AND due_date > ?"
		args = append(args, formatDate(*filter.DueAfter))
	}
	query += " ORDER BY id"

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
