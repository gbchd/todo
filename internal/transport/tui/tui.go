package tui

import (
	"context"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gbchd/todo/internal/service/todo"
)

// Run launches the interactive TUI in the given layout ("list", "split", or
// "kanban"; anything else falls back to "list") and blocks until the user
// quits.
func Run(ctx context.Context, svc *todo.Service, layout string, stdin io.Reader, stdout io.Writer) error {
	m := newModel(ctx, svc, ParseLayout(layout))

	// A session that cannot read the task list has nothing to show and no way
	// to get it, so it is refused here rather than started onto an empty
	// screen with an error under it. On a paired device this is how an
	// unreachable host reads: the same message the CLI prints, on the terminal
	// the user still has. Once a session is running the opposite rule applies
	// — a failure then is shown in the status line and the session lives.
	if m.err != nil {
		return m.err
	}

	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(stdin), tea.WithOutput(stdout))
	_, err := p.Run()
	return err
}
