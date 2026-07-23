// Package cli implements the `todo` command-line adapter over the shared
// core Service.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	tcli "github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

// TUILauncher starts the TUI adapter; wired to internal/transport/tui.Run
// by cmd/todo/main.go. Kept as an injected function so cli tests never
// launch an interactive program.
type TUILauncher func(ctx context.Context, svc *todo.Service, layout string, stdin io.Reader, stdout io.Writer) error

// ServeLauncher starts the web adapter; wired to internal/transport/web.Run.
type ServeLauncher func(ctx context.Context, svc *todo.Service, addr string, stdout io.Writer) error

// Run parses args and executes the matching todo subcommand, returning the
// process exit code. It resolves the SQLite database path (--db flag, else
// cfg.DBPath), opens the repository, and builds the Service itself, so
// tests can drive it end-to-end against a seeded temp-file database.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, cfg config.Config, runTUI TUILauncher, runServe ServeLauncher) int {
	dbPath := resolveDBFlag(args, cfg.DBPath)

	repo, err := repository.Open(ctx, dbPath)
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	defer repo.Close()
	svc := todo.NewService(repo)

	color := isTTY(stdout)
	root := buildRoot(svc, cfg, stdin, stdout, stderr, color, runTUI, runServe)

	if err := root.Run(ctx, args); err != nil {
		if ec, ok := err.(tcli.ExitCoder); ok {
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

func resolveDBFlag(args []string, fallback string) string {
	for i, a := range args {
		if a == "--db" && i+1 < len(args) {
			return args[i+1]
		}
		if rest, ok := strings.CutPrefix(a, "--db="); ok {
			return rest
		}
	}
	return fallback
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid task id %q", s)
	}
	return id, nil
}

func buildRoot(svc *todo.Service, cfg config.Config, stdin io.Reader, stdout, stderr io.Writer, color bool, runTUI TUILauncher, runServe ServeLauncher) *tcli.Command {
	return &tcli.Command{
		Name:      "todo",
		Usage:     "a personal todo app",
		Writer:    stdout,
		ErrWriter: stderr,
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "db", Usage: "path to the SQLite database", Value: cfg.DBPath},
		},
		ExitErrHandler: func(context.Context, *tcli.Command, error) {},
		Commands: []*tcli.Command{
			addCommand(svc, stdout, stderr),
			listCommand(svc, stdout, stderr, color),
			showCommand(svc, stdout, stderr, color),
			editCommand(svc, stdout, stderr),
			startCommand(svc, stdout, stderr),
			doneCommand(svc, stdout, stderr),
			reopenCommand(svc, stdout, stderr),
			deleteCommand(svc, stdin, stdout, stderr),
			tuiCommand(svc, cfg, stdin, stdout, runTUI),
			serveCommand(svc, cfg, stdout, runServe),
		},
	}
}

func addCommand(svc *todo.Service, stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:      "add",
		Usage:     "add a new task",
		ArgsUsage: "<title>",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "description"},
			&tcli.StringFlag{Name: "priority", Value: string(todo.PriorityNone)},
			&tcli.StringFlag{Name: "due"},
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

			t, err := svc.AddTask(ctx, input)
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			fmt.Fprintf(stdout, "Added task #%d: %s\n", t.ID, t.Title)
			return nil
		},
	}
}

func listCommand(svc *todo.Service, stdout, stderr io.Writer, color bool) *tcli.Command {
	return &tcli.Command{
		Name:  "list",
		Usage: "list tasks",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "status"},
			&tcli.StringFlag{Name: "priority"},
			&tcli.StringFlag{Name: "due-before"},
			&tcli.StringFlag{Name: "due-after"},
			&tcli.BoolFlag{Name: "all"},
			&tcli.StringFlag{Name: "sort"},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			filter := todo.TaskFilter{SortBy: todo.SortKey(cmd.String("sort"))}

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

			tasks, err := svc.ListTasks(ctx, filter)
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

func showCommand(svc *todo.Service, stdout, stderr io.Writer, color bool) *tcli.Command {
	return &tcli.Command{
		Name:      "show",
		Usage:     "show full detail for a task",
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			id, err := parseID(cmd.Args().First())
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			t, err := svc.GetTask(ctx, id)
			if err != nil {
				return reportErr(stderr, err, id)
			}
			printDetail(stdout, t, color)
			return nil
		},
	}
}

func editCommand(svc *todo.Service, stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:      "edit",
		Usage:     "edit a task",
		ArgsUsage: "<id>",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "title"},
			&tcli.StringFlag{Name: "description"},
			&tcli.StringFlag{Name: "priority"},
			&tcli.StringFlag{Name: "due"},
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

			t, err := svc.UpdateTask(ctx, id, patch)
			if err != nil {
				return reportErr(stderr, err, id)
			}
			fmt.Fprintf(stdout, "Updated task #%d: %s\n", t.ID, t.Title)
			return nil
		},
	}
}

func startCommand(svc *todo.Service, stdout, stderr io.Writer) *tcli.Command {
	return verbCommand("start", "mark a task in-progress", stdout, stderr, svc.StartTask)
}

func doneCommand(svc *todo.Service, stdout, stderr io.Writer) *tcli.Command {
	return verbCommand("done", "mark a task done", stdout, stderr, svc.CompleteTask)
}

func reopenCommand(svc *todo.Service, stdout, stderr io.Writer) *tcli.Command {
	return verbCommand("reopen", "reopen a done task", stdout, stderr, svc.ReopenTask)
}

func verbCommand(name, usage string, stdout, stderr io.Writer, verb func(context.Context, int64) (todo.Task, error)) *tcli.Command {
	return &tcli.Command{
		Name:      name,
		Usage:     usage,
		ArgsUsage: "<id>",
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			id, err := parseID(cmd.Args().First())
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			t, err := verb(ctx, id)
			if err != nil {
				return reportErr(stderr, err, id)
			}
			fmt.Fprintf(stdout, "Task #%d %s: %s\n", t.ID, t.Status, t.Title)
			return nil
		},
	}
}

func deleteCommand(svc *todo.Service, stdin io.Reader, stdout, stderr io.Writer) *tcli.Command {
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

			t, err := svc.GetTask(ctx, id)
			if err != nil {
				return reportErr(stderr, err, id)
			}

			if !cmd.Bool("force") {
				fmt.Fprintf(stdout, "Delete task #%d %q? [y/N] ", t.ID, t.Title)
				if !confirm(stdin) {
					fmt.Fprintln(stdout, "Aborted.")
					return nil
				}
			}

			if err := svc.DeleteTask(ctx, id); err != nil {
				return reportErr(stderr, err, id)
			}
			fmt.Fprintf(stdout, "Deleted task #%d\n", id)
			return nil
		},
	}
}

func confirm(r io.Reader) bool {
	var line string
	fmt.Fscanln(r, &line)
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func tuiCommand(svc *todo.Service, cfg config.Config, stdin io.Reader, stdout io.Writer, runTUI TUILauncher) *tcli.Command {
	return &tcli.Command{
		Name:  "tui",
		Usage: "launch the interactive terminal UI",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "layout", Value: cfg.TUILayout, Usage: "list|split|kanban"},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			return runTUI(ctx, svc, cmd.String("layout"), stdin, stdout)
		},
	}
}

func serveCommand(svc *todo.Service, cfg config.Config, stdout io.Writer, runServe ServeLauncher) *tcli.Command {
	return &tcli.Command{
		Name:  "serve",
		Usage: "launch the local web UI",
		Flags: []tcli.Flag{
			&tcli.IntFlag{Name: "port", Value: cfg.WebPort},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			addr := fmt.Sprintf("127.0.0.1:%d", cmd.Int("port"))
			return runServe(ctx, svc, addr, stdout)
		},
	}
}
