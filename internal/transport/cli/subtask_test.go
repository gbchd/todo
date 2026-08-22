package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/config"
)

// addParentAndChild seeds task #1 with subtask #2.
func addParentAndChild(t *testing.T, dbPath string) {
	t.Helper()
	_, _, code := runCLI(t, dbPath, "add", "parent")
	require.Equal(t, 0, code, "add parent")
	_, _, code = runCLI(t, dbPath, "add", "child", "--parent", "1")
	require.Equal(t, 0, code, "add child")
}

func TestAdd_WithParent(t *testing.T) {
	dbPath := newDB(t)
	addParentAndChild(t, dbPath)

	stdout, _, code := runCLI(t, dbPath, "show", "2")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Parent:      #1")
}

func TestAdd_ParentIsItselfASubtaskRejected(t *testing.T) {
	dbPath := newDB(t)
	addParentAndChild(t, dbPath)

	_, stderr, code := runCLI(t, dbPath, "add", "grandchild", "--parent", "2")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "parent:")
}

func TestEdit_DemoteAndPromote(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "parent")
	runCLI(t, dbPath, "add", "loose")

	_, _, code := runCLI(t, dbPath, "edit", "2", "--parent", "1")
	require.Equal(t, 0, code, "demote")
	stdout, _, _ := runCLI(t, dbPath, "show", "2")
	assert.Contains(t, stdout, "Parent:      #1")

	_, _, code = runCLI(t, dbPath, "edit", "2", "--parent", "none")
	require.Equal(t, 0, code, "promote")
	stdout, _, _ = runCLI(t, dbPath, "show", "2")
	assert.NotContains(t, stdout, "Parent:")
}

func TestList_HidesSubtasksByDefault(t *testing.T) {
	dbPath := newDB(t)
	addParentAndChild(t, dbPath)

	stdout, _, _ := runCLI(t, dbPath, "list")
	assert.NotContains(t, stdout, "child")
	assert.Contains(t, stdout, "parent (0/1)", "a hidden subtask must still roll up onto its parent's row")

	stdout, _, _ = runCLI(t, dbPath, "list", "--all")
	assert.Contains(t, stdout, "└ child")
}

func TestList_ByParent(t *testing.T) {
	dbPath := newDB(t)
	addParentAndChild(t, dbPath)
	runCLI(t, dbPath, "add", "unrelated")

	stdout, _, code := runCLI(t, dbPath, "list", "--parent", "1")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "child")
	assert.NotContains(t, stdout, "unrelated")
}

func TestDelete_PromptNamesSubtaskCount(t *testing.T) {
	dbPath := newDB(t)
	addParentAndChild(t, dbPath)
	runCLI(t, dbPath, "add", "second child", "--parent", "1")

	var outBuf, errBuf bytes.Buffer
	tui, serve, host := noopLaunchers()
	code := Run(context.Background(), []string{"todo", "--db", dbPath, "delete", "1"},
		strings.NewReader("y\n"), &outBuf, &errBuf, config.Config{}, tui, serve, host)
	require.Equal(t, 0, code, "stderr=%q", errBuf.String())
	assert.Contains(t, outBuf.String(), `Delete task #1 "parent" and its 2 subtasks? [y/N]`)

	_, _, code = runCLI(t, dbPath, "show", "2")
	assert.Equal(t, 1, code, "subtask must be gone with its parent")
}

// The prompt for a task with no subtasks is unchanged, so existing scripts and
// tests that match on it keep working.
func TestDelete_PromptUnchangedWithoutSubtasks(t *testing.T) {
	dbPath := newDB(t)
	runCLI(t, dbPath, "add", "lonely")

	var outBuf, errBuf bytes.Buffer
	tui, serve, host := noopLaunchers()
	Run(context.Background(), []string{"todo", "--db", dbPath, "delete", "1"},
		strings.NewReader("n\n"), &outBuf, &errBuf, config.Config{}, tui, serve, host)
	assert.Contains(t, outBuf.String(), `Delete task #1 "lonely"? [y/N]`)
}
