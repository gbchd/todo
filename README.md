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
todo list                                   # hides done tasks by default
todo list --all                             # show everything, including done
todo show 1
todo edit 1 --description "" --due none     # clear a field explicitly
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

Every subcommand accepts `--db <path>` to point at a specific SQLite file.

## Configuration

On first run, `todo` creates `~/.todo/config.toml` with the current defaults (SQLite file location, default TUI layout, default web port). Flags always override the config file; the config file overrides built-in defaults.

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
