package cli

import (
	"errors"
	"fmt"
	"io"

	tcli "github.com/urfave/cli/v3"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/repository/remote"
	"github.com/gbchd/todo/internal/service/todo"
)

// reportErr formats err per the app's error conventions, writes it to
// stderr, and returns a cli.ExitCoder so every application-level error
// exits 1 uniformly — as opposed to urfave/cli's own usage errors (bad
// flags, unknown commands), which keep the library's default exit code.
// id is the task id involved, if any (0 when not applicable).
func reportErr(stderr io.Writer, err error, id int64) error {
	fmt.Fprintln(stderr, "Error:", formatErr(err, id))
	return tcli.Exit(err, 1)
}

// formatErr turns an error into the one line the user reads.
//
// The four failures a paired device can hit — the host is unreachable, the
// credential was refused, the two builds disagree, another device got there
// first — arrive here already distinct and already naming the host, so this
// only has to keep them distinct and add the part the port cannot know: how
// this command was typed, and therefore that --local is the way to work
// without the host at all.
func formatErr(err error, id int64) string {
	if errors.Is(err, todo.ErrNotFound) {
		return fmt.Sprintf("task #%d not found", id)
	}
	var verr *todo.ValidationError
	if errors.As(err, &verr) {
		return fmt.Sprintf("%s: %s", verr.Field, verr.Message)
	}
	var cerr *todo.ConflictError
	if errors.As(err, &cerr) {
		return fmt.Sprintf("task #%d changed while this command was working on it (it was at version %d, and is now at %d); run the command again against the task as it is now",
			cerr.TaskID, cerr.Expected, cerr.Actual)
	}
	if errors.Is(err, remote.ErrUnreachable) {
		return fmt.Sprintf("%s; to work on this machine's own database instead, run the same command with --%s",
			err, config.LocalFlag)
	}
	return err.Error()
}
