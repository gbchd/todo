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
	if err != nil {
		t.Fatalf("open seed repo: %v", err)
	}
	defer repo.Close()
	for _, task := range tasks {
		if _, err := repo.Create(context.Background(), task); err != nil {
			t.Fatalf("seed create: %v", err)
		}
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
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "Added task #1: Buy milk") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestAdd_MissingTitle(t *testing.T) {
	dbPath := newDB(t)
	_, stderr, code := runCLI(t, dbPath, "add")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Error:") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestShow_NotFound(t *testing.T) {
	dbPath := newDB(t)
	_, stderr, code := runCLI(t, dbPath, "show", "42")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "task #42 not found") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestLifecycle(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task one")

	stdout, _, code := runCLI(t, dbPath, "start", "1")
	if code != 0 || !strings.Contains(stdout, "in-progress") {
		t.Fatalf("start: code=%d stdout=%q", code, stdout)
	}

	stdout, _, code = runCLI(t, dbPath, "done", "1")
	if code != 0 || !strings.Contains(stdout, "done") {
		t.Fatalf("done: code=%d stdout=%q", code, stdout)
	}

	stdout, _, code = runCLI(t, dbPath, "reopen", "1")
	if code != 0 || !strings.Contains(stdout, "open") {
		t.Fatalf("reopen: code=%d stdout=%q", code, stdout)
	}
}

func TestEdit_ClearDescription(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task", "--description", "desc")

	_, _, code := runCLI(t, dbPath, "edit", "1", "--description", "")
	if code != 0 {
		t.Fatalf("edit exit code = %d", code)
	}

	stdout, _, _ := runCLI(t, dbPath, "show", "1")
	if !strings.Contains(stdout, "(none)") {
		t.Errorf("expected cleared description, stdout=%q", stdout)
	}
}

func TestEdit_ClearDueDate(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task", "--due", "2026-08-01")

	_, _, code := runCLI(t, dbPath, "edit", "1", "--due", "none")
	if code != 0 {
		t.Fatalf("edit exit code = %d", code)
	}

	stdout, _, _ := runCLI(t, dbPath, "show", "1")
	if !strings.Contains(stdout, "Due:         -") {
		t.Errorf("expected cleared due date, stdout=%q", stdout)
	}
}

func TestDelete_ForceSkipsPrompt(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task")

	stdout, _, code := runCLI(t, dbPath, "delete", "1", "--force")
	if code != 0 || !strings.Contains(stdout, "Deleted task #1") {
		t.Fatalf("delete: code=%d stdout=%q", code, stdout)
	}

	_, stderr, code := runCLI(t, dbPath, "show", "1")
	if code != 1 || !strings.Contains(stderr, "not found") {
		t.Fatalf("expected not found after delete, stderr=%q", stderr)
	}
}

func TestDelete_PromptDeclined(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "task")

	var outBuf, errBuf bytes.Buffer
	tui, serve := noopLaunchers()
	code := Run(context.Background(), []string{"todo", "--db", dbPath, "delete", "1"}, strings.NewReader("n\n"), &outBuf, &errBuf, config.Config{}, tui, serve)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(outBuf.String(), "Aborted") {
		t.Errorf("stdout = %q", outBuf.String())
	}

	stdout, _, _ := runCLI(t, dbPath, "show", "1")
	if !strings.Contains(stdout, "task") {
		t.Errorf("task should still exist: %q", stdout)
	}
}

func TestList_HidesDoneByDefault(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "open task")
	runCLI(t, dbPath, "add", "done task")
	runCLI(t, dbPath, "done", "2")

	stdout, _, _ := runCLI(t, dbPath, "list")
	if strings.Contains(stdout, "done task") {
		t.Errorf("expected done task hidden, stdout=%q", stdout)
	}

	stdout, _, _ = runCLI(t, dbPath, "list", "--all")
	if !strings.Contains(stdout, "done task") {
		t.Errorf("expected done task with --all, stdout=%q", stdout)
	}
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
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	compareGolden(t, "list.golden", stdout)
}

func TestList_Golden_Empty(t *testing.T) {
	dbPath := newDB(t)
	stdout, _, code := runCLI(t, dbPath, "list")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
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
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	compareGolden(t, "show.golden", stdout)
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if got != string(want) {
		t.Errorf("output mismatch for %s\ngot:\n%s\nwant:\n%s", name, got, string(want))
	}
}
