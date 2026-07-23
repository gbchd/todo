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
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(stdin), tea.WithOutput(stdout))
	_, err := p.Run()
	return err
}
