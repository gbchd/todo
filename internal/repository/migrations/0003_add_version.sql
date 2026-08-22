-- A monotonic write counter, incremented by every UPDATE (see updateTask in
-- internal/repository/task.go). A caller that read a task can hand its version
-- back as a precondition and be told, rather than silently overwriting, when
-- someone else wrote in between.
--
-- NOT NULL DEFAULT 1 backfills every existing row in one statement: rows that
-- predate this migration are indistinguishable from freshly created ones, which
-- is correct — nobody holds an older version of them.
ALTER TABLE tasks ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
