package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

var update = flag.Bool("update", false, "update golden files")

func noopLaunchers() (TUILauncher, ServeLauncher) {
	return func(context.Context, *todo.Service, string, io.Reader, io.Writer) error { return nil },
		func(context.Context, *todo.Service, string, io.Writer) error { return nil }
}

func newDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "todo.db")
}

func seed(t *testing.T, dbPath string, tasks ...todo.Task) {
	t.Helper()
	repo, err := repository.Open(context.Background(), dbPath)
	require.NoError(t, err, "open seed repo")
	defer repo.Close()
	for _, task := range tasks {
		_, err := repo.Create(context.Background(), task)
		require.NoError(t, err, "seed create")
	}
}

func runCLI(t *testing.T, dbPath string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	tui, serve := noopLaunchers()
	full := append([]string{"todo", "--db", dbPath}, args...)
	code = Run(context.Background(), full, strings.NewReader(""), &outBuf, &errBuf, config.Config{}, tui, serve)
	return outBuf.String(), errBuf.String(), code
}

func TestAdd(t *testing.T) {
	dbPath := newDB(t)
	stdout, _, code := runCLI(t, dbPath, "add", "Buy milk", "--priority", "high")
	require.Equal(t, 0, code, "stdout=%s", stdout)
	assert.Contains(t, stdout, "Added task #1: Buy milk")
}

func TestAdd_MissingTitle(t *testing.T) {
	dbPath := newDB(t)
	_, stderr, code := runCLI(t, dbPath, "add")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "Error:")
}

// TestDBFlag_AllStyles guards against urfave/cli and any hand-rolled flag
// scan disagreeing about --db. All three styles urfave/cli itself accepts
// for a StringFlag must resolve to the same, single database path.
func TestDBFlag_AllStyles(t *testing.T) {
	tests := []struct {
		name   string
		dbArgs func(path string) []string
	}{
		{"double-dash space", func(p string) []string { return []string{"--db", p} }},
		{"double-dash equals", func(p string) []string { return []string{"--db=" + p} }},
		{"single-dash space", func(p string) []string { return []string{"-db", p} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := newDB(t)
			tui, serve := noopLaunchers()
			title := "task via " + tt.name

			var outBuf, errBuf bytes.Buffer
			addArgs := append([]string{"todo"}, tt.dbArgs(dbPath)...)
			addArgs = append(addArgs, "add", title)
			code := Run(context.Background(), addArgs, strings.NewReader(""), &outBuf, &errBuf, config.Config{}, tui, serve)
			require.Equal(t, 0, code, "add: stdout=%q stderr=%q", outBuf.String(), errBuf.String())

			_, statErr := os.Stat(dbPath)
			require.NoError(t, statErr, "expected the --db path to be created at %s", dbPath)

			outBuf.Reset()
			errBuf.Reset()
			listArgs := append([]string{"todo"}, tt.dbArgs(dbPath)...)
			listArgs = append(listArgs, "list")
			code = Run(context.Background(), listArgs, strings.NewReader(""), &outBuf, &errBuf, config.Config{}, tui, serve)
			require.Equal(t, 0, code, "list: stderr=%q", errBuf.String())
			assert.Contains(t, outBuf.String(), title, "task added via one invocation must be visible to a later invocation against the same --db path")
		})
	}
}

func TestShow_NotFound(t *testing.T) {
	dbPath := newDB(t)
	_, stderr, code := runCLI(t, dbPath, "show", "42")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "task #42 not found")
}

func TestLifecycle(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task one")

	stdout, _, code := runCLI(t, dbPath, "start", "1")
	require.Equal(t, 0, code, "start: stdout=%q", stdout)
	assert.Contains(t, stdout, "in-progress")

	stdout, _, code = runCLI(t, dbPath, "done", "1")
	require.Equal(t, 0, code, "done: stdout=%q", stdout)
	assert.Contains(t, stdout, "done")

	stdout, _, code = runCLI(t, dbPath, "reopen", "1")
	require.Equal(t, 0, code, "reopen: stdout=%q", stdout)
	assert.Contains(t, stdout, "open")
}

func TestEdit_ClearDescription(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task", "--description", "desc")

	_, _, code := runCLI(t, dbPath, "edit", "1", "--description", "")
	require.Equal(t, 0, code, "edit exit code")

	stdout, _, _ := runCLI(t, dbPath, "show", "1")
	assert.Contains(t, stdout, "(none)")
}

func TestEdit_ClearDueDate(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task", "--due", "2026-08-01")

	_, _, code := runCLI(t, dbPath, "edit", "1", "--due", "none")
	require.Equal(t, 0, code, "edit exit code")

	stdout, _, _ := runCLI(t, dbPath, "show", "1")
	assert.Contains(t, stdout, "Due:         -")
}

func TestDelete_ForceSkipsPrompt(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task")

	stdout, _, code := runCLI(t, dbPath, "delete", "1", "--force")
	require.Equal(t, 0, code, "delete: stdout=%q", stdout)
	assert.Contains(t, stdout, "Deleted task #1")

	_, stderr, code := runCLI(t, dbPath, "show", "1")
	require.Equal(t, 1, code, "expected not found after delete, stderr=%q", stderr)
	assert.Contains(t, stderr, "not found")
}

func TestDelete_PromptDeclined(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task")

	var outBuf, errBuf bytes.Buffer
	tui, serve := noopLaunchers()
	code := Run(context.Background(), []string{"todo", "--db", dbPath, "delete", "1"}, strings.NewReader("n\n"), &outBuf, &errBuf, config.Config{}, tui, serve)
	require.Equal(t, 0, code)
	assert.Contains(t, outBuf.String(), "Aborted")

	stdout, _, _ := runCLI(t, dbPath, "show", "1")
	assert.Contains(t, stdout, "task", "task should still exist")
}

func TestList_HidesDoneByDefault(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "open task")
	runCLI(t, dbPath, "add", "done task")
	runCLI(t, dbPath, "done", "2")

	stdout, _, _ := runCLI(t, dbPath, "list")
	assert.NotContains(t, stdout, "done task")

	stdout, _, _ = runCLI(t, dbPath, "list", "--all")
	assert.Contains(t, stdout, "done task")
}

func TestList_InvalidSortRejected(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task")

	_, stderr, code := runCLI(t, dbPath, "list", "--sort", "bogus")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "sort:")
}

func TestList_InvalidStatusRejected(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task")

	_, stderr, code := runCLI(t, dbPath, "list", "--status", "bogus")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "status:")
}

func TestList_Golden(t *testing.T) {
	dbPath := newDB(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	seed(t, dbPath,
		todo.Task{Title: "Write report", Status: todo.StatusOpen, Priority: todo.PriorityHigh, DueDate: &due, CreatedAt: now, UpdatedAt: now},
		todo.Task{Title: "Review PR", Status: todo.StatusInProgress, Priority: todo.PriorityNone, CreatedAt: now, UpdatedAt: now},
	)

	stdout, _, code := runCLI(t, dbPath, "list")
	require.Equal(t, 0, code)
	compareGolden(t, "list.golden", stdout)
}

func TestList_Golden_Empty(t *testing.T) {
	dbPath := newDB(t)
	stdout, _, code := runCLI(t, dbPath, "list")
	require.Equal(t, 0, code)
	compareGolden(t, "list_empty.golden", stdout)
}

func TestShow_Golden(t *testing.T) {
	dbPath := newDB(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	seed(t, dbPath, todo.Task{
		Title: "Write report", Description: "Quarterly numbers",
		Status: todo.StatusOpen, Priority: todo.PriorityHigh, DueDate: &due,
		CreatedAt: now, UpdatedAt: now,
	})

	stdout, _, code := runCLI(t, dbPath, "show", "1")
	require.Equal(t, 0, code)
	compareGolden(t, "show.golden", stdout)
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644), "write golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "read golden %s (run with -update to create it)", path)
	assert.Equal(t, string(want), got, "output mismatch for %s", name)
}
