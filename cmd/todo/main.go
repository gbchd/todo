// Command todo is a personal, single-user, single-machine todo app: one
// binary with CLI, TUI, and local web UI adapters sharing the same SQLite
// store.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/transport/cli"
	"github.com/gbchd/todo/internal/transport/tui"
	"github.com/gbchd/todo/internal/transport/web"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	return cli.Run(ctx, os.Args, os.Stdin, os.Stdout, os.Stderr, cfg, tui.Run, web.Run)
}
