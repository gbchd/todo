-- Adding a column with a REFERENCES clause is legal in SQLite's limited
-- ALTER TABLE only because the column defaults to NULL, which also backfills
-- every existing row as a top-level task.
--
-- ON DELETE CASCADE is only enforced when the connection sets
-- PRAGMA foreign_keys=ON (see internal/repository/sqlite.go); SQLite defaults
-- it off, and without it this constraint is decoration.
ALTER TABLE tasks ADD COLUMN parent_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE;

CREATE INDEX idx_tasks_parent_id ON tasks(parent_id);
