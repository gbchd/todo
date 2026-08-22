# todo

A personal, single-user todo app. One shared Go core (domain model + SQLite storage) behind three interfaces: a CLI, a terminal UI, and a local web UI. No accounts and no cloud: one machine keeps the list, and if you want it on a second machine, that first one serves it — see [Running a host](#running-a-host).

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

todo host                                   # serve this machine's list to your other devices
todo host pair                              # print a one-time code for a new device
todo pair http://host.local:8090 <code>     # ...and, on that device, use it
```

`--due`, `--due-before`, and `--due-after` accept `YYYY-MM-DD` or a relative shorthand: `today`, `tomorrow`, `+3d`, `+2w`.

## Subtasks

A subtask is just a task with a parent, one level deep — it has its own status, priority, and due date, and is completed independently. Completing a parent never cascades to its subtasks and completing the last subtask never completes the parent; the `2/5` progress on a parent's row is display only. Deleting a parent *does* delete its subtasks.

All three interfaces hide subtasks by default and roll them up onto their parent's row as `2/5`, with a `!` when one of the hidden subtasks is overdue. Reveal them with `todo list --all`, `s` in the TUI, or the **Subtasks** toggle in the web UI. Revealed subtasks are grouped under their parent rather than sorted globally, so `todo list --sort priority --all` is not a strictly descending priority column.

You always create a subtask from inside its parent: `--parent <id>` in the CLI, `enter` then `a` in the TUI, and the inline "Add a subtask" field in the web UI's task detail. See [ADR-0001](./docs/adr/0001-subtasks-as-self-referential-tasks.md).

Every subcommand accepts `--db <path>` to point at a specific SQLite file (on a device paired with a host, see `--local` under [Configuration](#choosing-a-backend)).

## Configuration

On first run, `todo` creates `~/.todo/config.toml` with the current defaults (SQLite file location, default TUI layout, default web port). Flags always override the config file; the config file overrides built-in defaults.

### Running a host

One machine can serve its list to the others. It runs the same binary:

```sh
todo host                                    # serve the task API, 127.0.0.1:8090 by default
todo host --addr 0.0.0.0:8090                # reachable from the network
todo host --db ~/lists/shared.db             # the database the host owns
todo host clients                            # which devices are registered, and since when
todo host revoke <device>                    # by name or credential id; takes effect immediately
```

The host keeps its own settings in `~/.todo/host.toml` — its listen address, the database it serves, and the registered devices. That file is separate from `config.toml` on purpose: the machine that hosts a list can also be a client of a different one, and `todo host --db` names the host's database, not the local client's.

Two rules are enforced rather than documented:

- **A non-loopback address with no device registered is refused.** Anyone who could reach it would be able to read and write the list, so `todo host --addr 0.0.0.0:8090` fails until at least one device has been paired. Pair the first device over loopback, then open the address up.
- **The host does not terminate TLS.** It speaks plain HTTP and is meant to be reached over a trusted network or from behind a reverse proxy that terminates TLS for it (nginx, Caddy, a tunnel). If you put one in front, pair with the `https://` URL — `todo pair` keeps the scheme you type, and everything after that goes to the proxy. Credentials travel in an `Authorization` header, so serving the host to the open internet without TLS in front of it hands them to whatever is on the path.

### Pairing a device

Pairing hands a device a credential without anyone copying a secret by hand. On the host:

```sh
todo host pair          # prints a one-time code and waits for a device to use it
```

The code is short-lived, single-use, and burned after a few wrong tries. On the new device, run the command the host printed:

```sh
todo pair http://host.local:8090 <code>
```

That rewrites the device's `config.toml` to point at the host and stores the credential it was issued. `todo host clients` then lists the device, and `todo host revoke` takes it off again.

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

### When a remote client cannot get through

Four things can go wrong on a paired device, and each says which one it is, names the host, and names the next step:

| What happened | What it says | What to do |
| --- | --- | --- |
| The host never answered | `cannot reach the todo host at <url>: ...` | start the host, or check the address; `--local` works on this machine's own database meanwhile |
| The host refused the credential | `the todo host rejected this device's credential: <url> said: ...` | the device was revoked or the secret is stale — pair it again |
| The two builds disagree | `this todo and the host do not speak the same protocol: <url> said: ...` | upgrade `todo` on the device or on the host |
| Another device got there first | `task #3 changed while this command was working on it` | re-read the task and run the command again |

Every request carries its own timeout, so a host that accepts a connection and then goes silent fails the command instead of wedging the terminal. The CLI exits non-zero, and `todo tui` refuses to open a session it cannot load. A TUI that is *already* open survives it: the failed write is reported on the status line, the tasks stay on screen, and the next read that succeeds clears both — nothing is cached, so a frame with no error on it was drawn from an answer the host gave just now. In the web UI the failure is shown as a message rather than as an empty list.

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
internal/repository/remote/      HTTP adapter for the same port — what a paired device uses instead
internal/config/                 ~/.todo/config.toml and host.toml loading, and backend selection
internal/credential/             device credentials: issuing them, and checking a presented token
internal/pairing/                the one-time code and the offer window `todo host pair` opens
internal/transport/cli/          `todo add|list|show|edit|start|done|reopen|delete` (urfave/cli)
internal/transport/tui/          `todo tui` — list/split/kanban layouts (bubbletea)
internal/transport/web/          `todo serve` — JSON API (net/http) + React/shadcn frontend
internal/transport/host/         `todo host` — the task API other devices talk to
```
