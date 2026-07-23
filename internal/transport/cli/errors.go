package cli

import (
	"errors"
	"fmt"
	"io"

	tcli "github.com/urfave/cli/v3"

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

func formatErr(err error, id int64) string {
	if errors.Is(err, todo.ErrNotFound) {
		return fmt.Sprintf("task #%d not found", id)
	}
	var verr *todo.ValidationError
	if errors.As(err, &verr) {
		return fmt.Sprintf("%s: %s", verr.Field, verr.Message)
	}
	return err.Error()
}
