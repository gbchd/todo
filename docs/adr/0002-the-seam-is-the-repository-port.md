# The client/server seam is the TaskRepository port

One instance of `todo` may own the SQLite file and expose it over HTTP; other
instances swap the SQLite adapter for an HTTP one and are otherwise unchanged.
The cut is the existing `TaskRepository` port, made on the *client* side: a
remote instance's `Service` talks to an HTTP repository, and the instance that
owns the database answers by running its *own* `Service` over its SQLite
repository. Domain logic therefore runs on both sides of the wire.

This commit is the half of that design the domain can see: tasks gain a
monotonic `Version`, `TaskPatch` gains an `ExpectedVersion` precondition, and a
mismatch is a `ConflictError` alongside `ErrNotFound` and `ValidationError`.

## Considered options

**Cutting at the `Service` instead** — shipping `AddTask`, `UpdateTask` and the
status verbs over the wire — is the obvious alternative, and it has one real
advantage: domain logic would exist in exactly one place, so a stale client
could not enforce a rule differently from the instance that owns the data. We
rejected it because it makes every transport talk to a different thing depending
on the backend. The CLI, the TUI and the web UI sit above the port and cannot
tell which adapter is underneath; above the `Service` they would have to. The
port is also the narrower interface — five methods, no optional fields, no
lifecycle rules — which makes it the easier one to reproduce faithfully over
HTTP and the easier one to hold to a shared contract suite.

Running the domain on both sides is the price, and it is deliberate rather than
accidental: the client's `Service` gives immediate validation without a
round-trip, and the owning instance's `Service` is what a stale or hand-rolled
client cannot bypass. The duplication is real; the rules it duplicates are small,
stable, and already covered by tests that run against both adapters.

**Offline operation with a sync engine** was rejected outright. A local cache
that diverges and later reconciles needs merge rules for every field, a tombstone
story for deletes, and a way to explain to the user what happened to an edit they
made on a train. Remote instances are online-only: no cache, no fallback to a
local file, no background reconciliation. When the backend is unreachable the
client says so and does nothing else. `ConflictError` exists to reject a lost
update, not to open the door to merging one.

**Last-write-wins**, the status quo, was the other option for concurrency. Two
devices editing the same task would silently keep whichever wrote second. That is
tolerable when both writes come from one machine seconds apart and intolerable
when the two writers are a laptop and a phone that disagree about what the task
says. A version precondition is the smallest thing that turns a lost update into
a visible error.

## Consequences

- **The port's atomicity guarantee now has two implementations.** Locally, `UpdateWith`
  runs fetch-mutate-write in one transaction. Remotely, a mutate closure cannot
  travel over the wire, so the adapter fetches, runs the closure locally, and
  writes guarded by the version it fetched — a precondition plus bounded retry
  standing in for the transaction. `ExpectedVersion` is what makes that
  substitution sound.
- **The comparison must happen inside the transaction.** Checking the version
  before calling `UpdateWith` would leave exactly the window the check exists to
  close. It therefore lives in the mutate closure, which the transaction has
  already loaded the row for. The closure still must not touch the repository:
  the SQLite pool is pinned to one connection and would deadlock.
- **`ExpectedVersion` is a precondition, not a mutation.** It does not
  participate in the patch's "did anything change" bookkeeping and does not by
  itself bump `UpdatedAt`. A patch that names the current version and changes
  nothing is as inert as an empty patch.
- **`Version` is owned by storage, not by callers.** `Create` seeds it at 1 and
  every write increments it in SQL; a `Task` handed in with a version is ignored,
  the same softening already accepted for the derived Subtask rollup fields.
- **The clock of whichever instance owns the database is authoritative** for
  `CreatedAt`, `UpdatedAt` and `CompletedAt`. Timestamps are re-derived there and
  whatever a remote instance sent is discarded. A device with a skewed clock
  would otherwise create a task that sorts wrongly forever, and no later edit
  from a correct clock could fix the ordering of a task it did not touch. The
  cost is that `Create` over HTTP returns a task whose timestamps differ from the
  one it was given, which the port's documentation has to state outright.
- **Local behaviour is unchanged.** Nothing in the CLI, TUI or web UI sets
  `ExpectedVersion`; an omitted precondition means the write is unconditional,
  exactly as before this change.
