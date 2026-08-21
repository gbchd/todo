// Package repository implements the todo.TaskRepository port against a
// SQLite database file. ISO-8601 parsing and NULL handling stay private
// here — the domain never learns SQLite exists.
package repository

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// SQLiteRepository implements todo.TaskRepository against a SQLite file.
type SQLiteRepository struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path, applies
// connection pragmas, and runs any pending migrations.
func Open(ctx context.Context, path string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// busy_timeout is a per-connection setting, not persisted in the database
	// file the way journal_mode is; pinning the pool to one connection keeps
	// every statement on the connection that had pragmas applied to it.
	db.SetMaxOpenConns(1)

	// foreign_keys is load-bearing, not hygiene: tasks.parent_id is
	// ON DELETE CASCADE, and SQLite defaults this pragma off per connection,
	// so without it deleting a Parent Task silently orphans its Subtasks
	// instead of removing them.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}

	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteRepository{db: db}, nil
}

// Close closes the underlying database connection.
func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}
