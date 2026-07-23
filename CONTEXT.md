# Personal Todo App

A single-user, single-machine task tracker: one shared domain model for a Task and its lifecycle, consumed identically by three interchangeable interfaces (CLI, TUI, web).

## Language

**Task**
A single actionable to-do item — the only entity in this domain. Identified by a permanent, auto-incrementing id that is never reused or renumbered after deletion.
_Avoid_: Item, todo, entry

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
