# Subtasks are Tasks with a parent, not a separate entity

A Task may now carry a nullable `parent_id` pointing at another Task, limited to
one level of nesting. A Subtask is therefore a Task in every respect — same id
sequence, same status lifecycle, its own priority and due date, independently
startable and completable — rather than a lighter-weight entity living in its own
table.

## Considered options

**A checklist-item table** (`title`, `done`, `position`, `task_id`) was the
alternative, and it is genuinely cheaper: it leaves `ListTasks` and every
transport's rendering completely untouched, because checklist items are not Tasks
and never appear in a task list, on the board, or in a filter. The self-referential
design instead forces a top-level-vs-all distinction into `TaskFilter`, the CLI,
the TUI, and the web UI all at once.

We chose self-reference because the intended use is to *work* a subtask — start
it, give it its own due date, complete it on its own schedule. A checklist item
can only be ticked, can never be promoted to a real Task when it turns out to be
bigger than expected, and would have made "the Task is the only entity" false in
a more invasive way than adding a self-reference does.

## Consequences

- **`Task` gains derived, read-only fields** (`ChildCount`, `DoneChildCount`,
  `AnyChildOverdue`) so a parent row can roll up its children's state. `Task` is
  also the write model, so these are populated on read and ignored on write —
  a deliberate softening of the "maps rows to and from Task" contract, accepted
  to avoid a parallel `TaskWithProgress` type rippling through every transport.
- **`TaskFilter.ParentID` must be `Optional[*int64]`, not `*int64`.** A plain
  pointer cannot distinguish "any parent" from "no parent"; both are nil.
- **`PRAGMA foreign_keys=ON` becomes load-bearing.** The connection did not set
  it, and SQLite defaults it off per-connection, so `ON DELETE CASCADE` would
  parse, migrate cleanly, and silently orphan every child. This is the one
  failure mode in the change that unit tests against the fake repository cannot
  catch.
- **The one-level rule is enforced in `Service`, not in SQL.** SQLite cannot
  express "the row my foreign key points at must itself have a NULL foreign key"
  without a trigger. The same rule also makes a separate cycle check unnecessary.
- Reverses the "subtasks are out of scope" decision recorded in the spec map
  (#1) and "Task domain model & lifecycle" (#2).
