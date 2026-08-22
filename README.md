# todo

A personal, single-user, single-machine todo app. One shared Go core (domain model + SQLite storage) behind three interfaces: a CLI, a terminal UI, and a local web UI. No accounts, no sync, no server to run anywhere but your own machine.

See [CONTEXT.md](./CONTEXT.md) for the domain vocabulary and [docs/adr/](./docs/adr/) for the reasoning behind notable technical decisions.

## Install / build

Requires Go 1.26+. The web UI's frontend also requires Node 20+ (only to build it — the resulting binary is self-contained).

```sh
make build        # builds the frontend, then the todo binary, into bin/todo
```

Or by hand:

```sh
npm --prefix internal/transport/web/frontend ci
npm --prefix internal/transport/web/frontend run build
go build -o bin/todo ./cmd/todo
```

The frontend build writes straight into `internal/transport/web/static`, which `go build` embeds into the binary — so `go build ./cmd/todo` alone works too, as long as `static/` already has built assets in it (true right after cloning; only stale if you've changed the frontend since).

## Usage

```sh
todo add "Write the quarterly report" --priority high --due 2026-08-01
todo list                                   # hides done tasks and subtasks by default
todo list --all                             # show everything: done tasks and subtasks
todo show 1
todo edit 1 --description "" --due none     # clear a field explicitly

todo add "Draft the outline" --parent 1     # add a subtask of task 1
todo list --parent 1                        # only task 1's subtasks
todo edit 2 --parent 1                      # demote an existing task to a subtask
todo edit 2 --parent none                   # promote it back to top level
todo start 1
todo done 1
todo reopen 1
todo delete 1 --force

todo tui                                    # interactive terminal UI
todo tui --layout kanban                    # list | split | kanban

todo serve                                  # local web UI at http://127.0.0.1:<port>
todo serve --port 9000
```

`--due`, `--due-before`, and `--due-after` accept `YYYY-MM-DD` or a relative shorthand: `today`, `tomorrow`, `+3d`, `+2w`.

## Subtasks

A subtask is just a task with a parent, one level deep — it has its own status, priority, and due date, and is completed independently. Completing a parent never cascades to its subtasks and completing the last subtask never completes the parent; the `2/5` progress on a parent's row is display only. Deleting a parent *does* delete its subtasks.

All three interfaces hide subtasks by default and roll them up onto their parent's row as `2/5`, with a `!` when one of the hidden subtasks is overdue. Reveal them with `todo list --all`, `s` in the TUI, or the **Subtasks** toggle in the web UI. Revealed subtasks are grouped under their parent rather than sorted globally, so `todo list --sort priority --all` is not a strictly descending priority column.

You always create a subtask from inside its parent: `--parent <id>` in the CLI, `enter` then `a` in the TUI, and the inline "Add a subtask" field in the web UI's task detail. See [ADR-0001](./docs/adr/0001-subtasks-as-self-referential-tasks.md).

Every subcommand accepts `--db <path>` to point at a specific SQLite file (on a device paired with a host, see `--local` under [Configuration](#choosing-a-backend)).

## Configuration

On first run, `todo` creates `~/.todo/config.toml` with the current defaults (SQLite file location, default TUI layout, default web port). Flags always override the config file; the config file overrides built-in defaults.

### Choosing a backend

`config.toml` also says where this machine's tasks live:

```toml
[backend]
kind = "local"        # the default: the SQLite file db_path names, on this machine
```

`todo pair <host url> <code>` rewrites that block to `kind = "remote"` with the host's URL and the credential the host issued. A remote client keeps nothing locally — every command reads and writes the host's list — and it is online-only: if the host cannot be reached, the command says so and does nothing else.

Three things can override the file, narrowest last:

```sh
export TODO_HOST_SECRET=...            # supply the credential from the environment
                                       # instead of keeping it in config.toml
todo --local list                      # use this machine's own database, just this once
todo --local --db ~/old/todo.db list   # ...or a specific file
```

Pairing never migrates, imports, or touches an existing local database — it is left exactly where it was, and `--local` is how you read it again. Passing `--db` on a paired device *without* `--local` is an error rather than a silent choice between two different task lists.

## Development

```sh
make test                                    # go test ./... — service/repo/cli/tui/web
npm --prefix internal/transport/web/frontend run lint
```

Frontend hot-reload workflow: run `todo serve` in one terminal (owns the JSON API) and `make frontend-dev` in another (Vite dev server on :5173, proxying `/api/*` to the running `todo serve`).

## Layout

```
cmd/todo/                        entry point: wires config → repository → service → transport
internal/service/todo/           domain model (Task) + Service — the shared core
internal/repository/             SQLite adapter (implements the Service's TaskRepository port)
internal/config/                 ~/.todo/config.toml loading
internal/transport/cli/          `todo add|list|show|edit|start|done|reopen|delete` (urfave/cli)
internal/transport/tui/          `todo tui` — list/split/kanban layouts (bubbletea)
internal/transport/web/          `todo serve` — JSON API (net/http) + React/shadcn frontend
internal/transport/statusverb/   shared status→verb mapping used by the TUI and web adapters
```
