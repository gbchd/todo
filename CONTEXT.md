# Personal Todo App

A single-user, multi-device task tracker: one shared domain model for a Task and its lifecycle, consumed identically by three interchangeable interfaces (CLI, TUI, web).

## Language

**Task**
A single actionable to-do item — the only kind of entity in this domain. Identified by an auto-incrementing id that is permanent within a store — never reused or renumbered after deletion — but means nothing outside the store that issued it. A Task may have a parent, or may have Subtasks of its own, but never both.
_Avoid_: Item, todo, entry

**Subtask**
A role a Task plays, not a separate entity: a Task that has a parent. A Subtask is a Task in every respect — it has its own Status, Priority, and Due Date, and is worked and completed independently of its parent.
_Avoid_: Child, step, checklist item

**Parent Task**
A Task that has Subtasks. Its Status is never derived from theirs — a Parent Task may be `done` while its Subtasks are still `open`, and completing the last Subtask never completes the parent.
_Avoid_: Epic, group, container

**Status**
A Task's place in its lifecycle: `open` (not started), `in-progress` (actively being worked), or `done` (finished). A Task begins `open`; reopening a `done` Task returns it to `open`, never directly to `in-progress`.
_Avoid_: State, stage

**Priority**
How urgently a Task should be worked, ranked `high` > `medium` > `low` > `none`. Defaults to `none` and is set explicitly — it never escalates on its own as a Due Date approaches.
_Avoid_: Urgency, importance

**Due Date**
The calendar day by which a Task should be finished. Optional, day-granularity only (no time-of-day component) — a Task with no Due Date is never "overdue."
_Avoid_: Deadline

**Completion**
The moment a Task's Status becomes `done`. Reopening a Task clears its Completion — a Task only ever remembers its most recent completion, not a history of past completions and reopenings.
_Avoid_: Finishing, closing
