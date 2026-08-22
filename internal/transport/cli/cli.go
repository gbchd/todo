// Package cli implements the `todo` command-line adapter over the shared
// core Service.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	tcli "github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/credential"
	"github.com/gbchd/todo/internal/pairing"
	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/repository/remote"
	"github.com/gbchd/todo/internal/service/todo"
)

// serviceHolder gives subcommand closures a stable handle onto the Service,
// which doesn't exist yet when the command tree is built: the root
// command's Before hook is what resolves --db and constructs it.
type serviceHolder struct {
	svc *todo.Service
}

// TUILauncher starts the TUI adapter; wired to internal/transport/tui.Run
// by cmd/todo/main.go. Kept as an injected function so cli tests never
// launch an interactive program.
type TUILauncher func(ctx context.Context, svc *todo.Service, layout string, stdin io.Reader, stdout io.Writer) error

// ServeLauncher starts the web adapter; wired to internal/transport/web.Run.
type ServeLauncher func(ctx context.Context, svc *todo.Service, addr string, stdout io.Writer) error

// HostLauncher starts the host adapter; wired to internal/transport/host.Run.
// creds is how the host resolves the device a request's token names, and pairs
// is the outstanding pairing offer `todo host pair` opens in another process.
type HostLauncher func(ctx context.Context, svc *todo.Service, addr string, creds credential.Source, pairs *pairing.Store, stdout io.Writer) error

// Run parses args and executes the matching todo subcommand, returning the
// process exit code. urfave/cli is the single parser for --db: the root
// command's Before hook opens the repository and builds the Service once
// the flag's real, fully-parsed value is available, so tests can drive
// this end-to-end against a seeded temp-file database.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, cfg config.Config, runTUI TUILauncher, runServe ServeLauncher, runHost HostLauncher) int {
	holder := &serviceHolder{}
	color := isTTY(stdout)
	root := buildRoot(holder, cfg, stdin, stdout, stderr, color, runTUI, runServe, runHost)

	if err := root.Run(ctx, args); err != nil {
		var ec tcli.ExitCoder
		if errors.As(err, &ec) {
			return ec.ExitCode()
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid task id %q", s)
	}
	return id, nil
}

func buildRoot(holder *serviceHolder, cfg config.Config, stdin io.Reader, stdout, stderr io.Writer, color bool, runTUI TUILauncher, runServe ServeLauncher, runHost HostLauncher) *tcli.Command {
	var repo *repository.SQLiteRepository

	return &tcli.Command{
		Name:      "todo",
		Usage:     "a personal todo app",
		Writer:    stdout,
		ErrWriter: stderr,
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "db", Usage: "path to the SQLite database", Value: cfg.DBPath},
			&tcli.BoolFlag{
				Name:  config.LocalFlag,
				Usage: "use this machine's own database for this command, even when paired with a host",
			},
		},
		ExitErrHandler: func(context.Context, *tcli.Command, error) {},
		Before: func(ctx context.Context, cmd *tcli.Command) (context.Context, error) {
			// Two commands are not about this machine's own task list, and
			// opening the client's database for them would leave a stray empty
			// todo.db behind: `todo host` owns a different database — the one
			// named in host.toml — and opens it itself, and `todo pair` runs on
			// a device that is about to stop having a local database at all.
			if needsNoRepository(cmd.Args().First()) {
				return ctx, nil
			}

			// Which backend this command speaks to is settled before anything
			// is opened, so that a refusal — a --db against a paired device,
			// a host with no credential — never first creates a database it
			// then declines to use.
			selection, err := config.SelectBackend(cfg, config.Override{
				Local:     cmd.Bool(config.LocalFlag),
				DBPath:    cmd.String("db"),
				DBPathSet: cmd.IsSet("db"),
				Secret:    os.Getenv(config.SecretEnv),
			})
			if err != nil {
				fmt.Fprintln(stderr, "Error:", err)
				return ctx, tcli.Exit(err, 1)
			}

			if selection.Remote {
				holder.svc = todo.NewService(remote.New(selection.HostURL, selection.Secret))
				return ctx, nil
			}

			r, err := repository.Open(ctx, selection.DBPath)
			if err != nil {
				fmt.Fprintln(stderr, "Error:", err)
				return ctx, tcli.Exit(err, 1)
			}
			repo = r
			holder.svc = todo.NewService(repo)
			return ctx, nil
		},
		After: func(context.Context, *tcli.Command) error {
			if repo != nil {
				_ = repo.Close()
			}
			return nil
		},
		Commands: []*tcli.Command{
			addCommand(holder, stdout, stderr),
			listCommand(holder, stdout, stderr, color),
			showCommand(holder, stdout, stderr, color),
			editCommand(holder, stdout, stderr),
			startCommand(holder, stdout, stderr),
			doneCommand(holder, stdout, stderr),
			reopenCommand(holder, stdout, stderr),
			deleteCommand(holder, stdin, stdout, stderr),
			tuiCommand(holder, cfg, stdin, stdout, runTUI),
			serveCommand(holder, cfg, stdout, runServe),
			hostCommand(stdout, stderr, runHost),
			pairCommand(stdout, stderr),
		},
	}
}

// needsNoRepository reports whether a subcommand runs without the client's
// task database.
func needsNoRepository(name string) bool {
	return name == hostCommandName || name == pairCommandName
}

func addCommand(holder *serviceHolder, stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:      "add",
		Usage:     "add a new task",
		ArgsUsage: "<title>",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "description"},
			&tcli.StringFlag{Name: "priority", Value: string(todo.PriorityNone)},
			&tcli.StringFlag{Name: "due"},
			&tcli.IntFlag{Name: "parent", Usage: "id of the task this is a subtask of"},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			title := cmd.Args().First()
			if title == "" {
				return reportErr(stderr, fmt.Errorf("missing task title"), 0)
			}

			input := todo.NewTask{
				Title:       title,
				Description: cmd.String("description"),
				Priority:    todo.Priority(cmd.String("priority")),
			}
			if cmd.IsSet("due") {
				d, err := parseDate(cmd.String("due"), time.Now())
				if err != nil {
					return reportErr(stderr, err, 0)
				}
				input.DueDate = &d
			}
			if cmd.IsSet("parent") {
				parent := int64(cmd.Int("parent"))
				input.ParentID = &parent
			}

			t, err := holder.svc.AddTask(ctx, input)
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			fmt.Fprintf(stdout, "Added task #%d: %s\n", t.ID, t.Title)
			return nil
		},
	}
}

func listCommand(holder *serviceHolder, stdout, stderr io.Writer, color bool) *tcli.Command {
	return &tcli.Command{
		Name:  "list",
		Usage: "list tasks",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "status"},
			&tcli.StringFlag{Name: "priority"},
			&tcli.StringFlag{Name: "due-before"},
			&tcli.StringFlag{Name: "due-after"},
			&tcli.BoolFlag{Name: "all", Usage: "show everything: done tasks and subtasks too"},
			&tcli.IntFlag{Name: "parent", Usage: "show only the subtasks of this task"},
			&tcli.StringFlag{Name: "sort"},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			filter := todo.TaskFilter{SortBy: todo.SortKey(cmd.String("sort"))}

			// Subtasks are hidden through the filter rather than after the
			// fact (the way done tasks are) because a hidden subtask still
			// has to reach its parent's row as a rolled-up count.
			switch {
			case cmd.IsSet("parent"):
				parent := int64(cmd.Int("parent"))
				filter.ParentID = todo.Set(&parent)
			case !cmd.Bool("all"):
				filter.ParentID = todo.Set[*int64](nil)
			}

			if cmd.IsSet("status") {
				s := todo.Status(cmd.String("status"))
				filter.Status = &s
			}
			if cmd.IsSet("priority") {
				p := todo.Priority(cmd.String("priority"))
				filter.Priority = &p
			}
			if cmd.IsSet("due-before") {
				d, err := parseDate(cmd.String("due-before"), time.Now())
				if err != nil {
					return reportErr(stderr, err, 0)
				}
				filter.DueBefore = &d
			}
			if cmd.IsSet("due-after") {
				d, err := parseDate(cmd.String("due-after"), time.Now())
				if err != nil {
					return reportErr(stderr, err, 0)
				}
				filter.DueAfter = &d
			}

			tasks, err := holder.svc.ListTasks(ctx, filter)
			if err != nil {
				return reportErr(stderr, err, 0)
			}

			if !cmd.IsSet("status") && !cmd.Bool("all") {
				tasks = hideDone(tasks)
			}

			printTable(stdout, tasks, color)
			return nil
		},
	}
}

func hideDone(tasks []todo.Task) []todo.Task {
	out := tasks[:0:0]
	for _, t := range tasks {
		if t.Status != todo.StatusDone {
			out = append(out, t)
		}
	}
	return out
}

func showCommand(holder *serviceHolder, stdout, stderr io.Writer, color bool) *tcli.Command {
	return &tcli.Command{
		Name:      "show",
		Usage:     "show full detail for a task",
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			id, err := parseID(cmd.Args().First())
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			t, err := holder.svc.GetTask(ctx, id)
			if err != nil {
				return reportErr(stderr, err, id)
			}
			printDetail(stdout, t, color)
			return nil
		},
	}
}

func editCommand(holder *serviceHolder, stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:      "edit",
		Usage:     "edit a task",
		ArgsUsage: "<id>",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "title"},
			&tcli.StringFlag{Name: "description"},
			&tcli.StringFlag{Name: "priority"},
			&tcli.StringFlag{Name: "due"},
			&tcli.StringFlag{Name: "parent", Usage: "id of the parent task, or \"none\" to promote to top level"},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			id, err := parseID(cmd.Args().First())
			if err != nil {
				return reportErr(stderr, err, 0)
			}

			patch := todo.TaskPatch{}
			if cmd.IsSet("title") {
				patch.Title = todo.Set(cmd.String("title"))
			}
			if cmd.IsSet("description") {
				patch.Description = todo.Set(cmd.String("description"))
			}
			if cmd.IsSet("priority") {
				patch.Priority = todo.Set(todo.Priority(cmd.String("priority")))
			}
			if cmd.IsSet("due") {
				v := cmd.String("due")
				if v == "none" {
					patch.DueDate = todo.Set[*time.Time](nil)
				} else {
					d, err := parseDate(v, time.Now())
					if err != nil {
						return reportErr(stderr, err, 0)
					}
					patch.DueDate = todo.Set(&d)
				}
			}
			if cmd.IsSet("parent") {
				v := cmd.String("parent")
				if v == "none" {
					patch.ParentID = todo.Set[*int64](nil)
				} else {
					parent, err := parseID(v)
					if err != nil {
						return reportErr(stderr, err, 0)
					}
					patch.ParentID = todo.Set(&parent)
				}
			}

			t, err := holder.svc.UpdateTask(ctx, id, patch)
			if err != nil {
				return reportErr(stderr, err, id)
			}
			fmt.Fprintf(stdout, "Updated task #%d: %s\n", t.ID, t.Title)
			return nil
		},
	}
}

func startCommand(holder *serviceHolder, stdout, stderr io.Writer) *tcli.Command {
	return verbCommand("start", "mark a task in-progress", holder, stdout, stderr, (*todo.Service).StartTask)
}

func doneCommand(holder *serviceHolder, stdout, stderr io.Writer) *tcli.Command {
	return verbCommand("done", "mark a task done", holder, stdout, stderr, (*todo.Service).CompleteTask)
}

func reopenCommand(holder *serviceHolder, stdout, stderr io.Writer) *tcli.Command {
	return verbCommand("reopen", "reopen a done task", holder, stdout, stderr, (*todo.Service).ReopenTask)
}

// verbCommand builds a single-argument lifecycle command (start/done/reopen).
// verb is a method expression (e.g. (*todo.Service).StartTask) rather than a
// bound method value, since holder.svc isn't populated until the root
// command's Before hook fires — well after this command tree is built.
func verbCommand(name, usage string, holder *serviceHolder, stdout, stderr io.Writer, verb func(*todo.Service, context.Context, int64) (todo.Task, error)) *tcli.Command {
	return &tcli.Command{
		Name:      name,
		Usage:     usage,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			id, err := parseID(cmd.Args().First())
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			t, err := verb(holder.svc, ctx, id)
			if err != nil {
				return reportErr(stderr, err, id)
			}
			fmt.Fprintf(stdout, "Task #%d %s: %s\n", t.ID, t.Status, t.Title)
			return nil
		},
	}
}

func deleteCommand(holder *serviceHolder, stdin io.Reader, stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:      "delete",
		Usage:     "delete a task",
		ArgsUsage: "<id>",
		Flags: []tcli.Flag{
			&tcli.BoolFlag{Name: "force", Aliases: []string{"f"}},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			id, err := parseID(cmd.Args().First())
			if err != nil {
				return reportErr(stderr, err, 0)
			}

			t, err := holder.svc.GetTask(ctx, id)
			if err != nil {
				return reportErr(stderr, err, id)
			}

			if !cmd.Bool("force") {
				// --force stays absolute: refusing on a task with subtasks
				// would defeat the flag's only purpose.
				fmt.Fprintf(stdout, "Delete task #%d %q%s? [y/N] ", t.ID, t.Title, subtaskSuffix(t.ChildCount))
				if !confirm(stdin) {
					fmt.Fprintln(stdout, "Aborted.")
					return nil
				}
			}

			if err := holder.svc.DeleteTask(ctx, id); err != nil {
				return reportErr(stderr, err, id)
			}
			fmt.Fprintf(stdout, "Deleted task #%d\n", id)
			return nil
		},
	}
}

// subtaskSuffix is empty for a task with no subtasks, so the long-standing
// delete prompt is unchanged byte for byte in the common case.
func subtaskSuffix(childCount int) string {
	switch childCount {
	case 0:
		return ""
	case 1:
		return " and its 1 subtask"
	default:
		return fmt.Sprintf(" and its %d subtasks", childCount)
	}
}

func confirm(r io.Reader) bool {
	var line string
	fmt.Fscanln(r, &line)
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func tuiCommand(holder *serviceHolder, cfg config.Config, stdin io.Reader, stdout io.Writer, runTUI TUILauncher) *tcli.Command {
	return &tcli.Command{
		Name:  "tui",
		Usage: "launch the interactive terminal UI",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "layout", Value: cfg.TUILayout, Usage: "list|split|kanban"},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			return runTUI(ctx, holder.svc, cmd.String("layout"), stdin, stdout)
		},
	}
}

// hostCommandName is matched in the root Before hook, so it lives next to the
// command it names.
const hostCommandName = "host"

// hostCommand serves the task API over HTTP from the host's own settings file.
//
// It opens its own repository instead of using the root command's: host.toml
// names the database the host owns, which is deliberately not the one
// config.toml names for the local client, so that one machine can host one
// list and use another. Flags override the file, and the resulting settings
// are validated before anything is opened or bound — a refusal to listen must
// not first create a database.
//
// `todo host pair`, `todo host clients` and `todo host revoke` mount here as
// subcommands. They manage the same file this action reads, which is why none
// of them takes a path: host.toml is the host's, singular.
func hostCommand(stdout, stderr io.Writer, runHost HostLauncher) *tcli.Command {
	return &tcli.Command{
		Name:  hostCommandName,
		Usage: "serve the task API over HTTP",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "addr", Usage: "address to listen on"},
			&tcli.StringFlag{Name: "db", Usage: "path to the SQLite database the host owns"},
		},
		Commands: []*tcli.Command{
			hostPairCommand(stdout, stderr),
			hostClientsCommand(stdout, stderr),
			hostRevokeCommand(stdout, stderr),
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			hostCfg, err := config.LoadHost()
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			if cmd.IsSet("addr") {
				hostCfg.ListenAddr = cmd.String("addr")
			}
			if cmd.IsSet("db") {
				hostCfg.DBPath = cmd.String("db")
			}
			if err := hostCfg.Validate(); err != nil {
				return reportErr(stderr, err, 0)
			}

			repo, err := repository.Open(ctx, hostCfg.DBPath)
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			defer repo.Close()

			dir, err := config.Dir()
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			// The store is read per request, so a code `todo host pair` opens
			// minutes after the host started is still seen by it.
			pairs := pairing.NewStore(dir, registerDevice)

			return runHost(ctx, todo.NewService(repo), hostCfg.ListenAddr, hostCredentials, pairs, stdout)
		},
	}
}

// hostCredentials resolves the device a token names by reading host.toml, and
// is called once per authenticated request rather than once at startup.
//
// Revoking a device that was lost this morning has to take effect this
// morning; a revocation that waits for the operator to remember to restart the
// host is not a revocation. Re-reading a handful of lines of TOML costs
// nothing beside the password-hash verification that immediately follows it.
// A file that cannot be read denies the request, because a credential store
// that fails open is worse than one that is down.
func hostCredentials(id string) (credential.Credential, bool) {
	cfg, err := config.LoadHost()
	if err != nil {
		return credential.Credential{}, false
	}
	return cfg.Credential(id)
}

// hostClientsCommand lists the devices registered with this host.
//
// It prints each device's name, its credential id, and when it was added, and
// deliberately nothing else: the file it reads holds a password hash per
// device and no usable secret, and printing the hash would only invite someone
// to paste it somewhere. The id is not secret — it is half of a token, and the
// half that proves nothing — and is shown because revoke needs it whenever two
// devices share a name.
func hostClientsCommand(stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:  "clients",
		Usage: "list the devices registered with this host",
		Action: func(_ context.Context, _ *tcli.Command) error {
			cfg, err := config.LoadHost()
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			printClients(stdout, cfg.Clients)
			return nil
		},
	}
}

// hostRevokeCommand removes one device's credential, leaving every other
// device working. There is no confirmation prompt: revoking is the safe
// direction, and the cost of a mistake is one re-pairing.
func hostRevokeCommand(stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:      "revoke",
		Usage:     "remove one device's credential",
		ArgsUsage: "<device name or id>",
		Action: func(_ context.Context, cmd *tcli.Command) error {
			target := cmd.Args().First()
			if target == "" {
				return reportErr(stderr, errors.New("missing device: name or id of the device to revoke"), 0)
			}
			cfg, err := config.LoadHost()
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			removed, err := cfg.RemoveClient(target)
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			if err := config.SaveHost(cfg); err != nil {
				return reportErr(stderr, err, 0)
			}
			fmt.Fprintf(stdout, "Revoked %s (%s). Every other device keeps working.\n", removed.Name, removed.ID)
			return nil
		},
	}
}

func serveCommand(holder *serviceHolder, cfg config.Config, stdout io.Writer, runServe ServeLauncher) *tcli.Command {
	return &tcli.Command{
		Name:  "serve",
		Usage: "launch the local web UI",
		Flags: []tcli.Flag{
			&tcli.IntFlag{Name: "port", Value: cfg.WebPort},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			addr := fmt.Sprintf("127.0.0.1:%d", cmd.Int("port"))
			return runServe(ctx, holder.svc, addr, stdout)
		},
	}
}
