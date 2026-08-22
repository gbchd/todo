// The behaviour this adapter shares with every other implementation of the
// todo.TaskRepository port lives in the contract suite (see
// contract_test.go). What stays here is what only SQLite can be asked about:
// the migrated schema version, persistence across a close and reopen, and the
// unexported helpers no port-level caller can reach.

package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/service/todo"
)

func openTestRepo(t *testing.T) *SQLiteRepository {
	t.Helper()
	path := filepath.Join(t.TempDir(), "todo.db")
	repo, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestOpen_Migrates(t *testing.T) {
	repo := openTestRepo(t)
	var version int
	require.NoError(t, repo.db.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version))
	assert.Equal(t, 2, version, "user_version must match the highest embedded migration")
}

func TestOpen_ReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.db")
	ctx := context.Background()

	repo1, err := Open(ctx, path)
	require.NoError(t, err, "first Open")
	created, err := repo1.Create(ctx, todo.Task{
		Title: "persisted", Status: todo.StatusOpen, Priority: todo.PriorityNone,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	require.NoError(t, err)
	repo1.Close()

	repo2, err := Open(ctx, path)
	require.NoError(t, err, "second Open")
	defer repo2.Close()

	got, err := repo2.Get(ctx, created.ID)
	require.NoError(t, err, "Get after reopen")
	assert.Equal(t, "persisted", got.Title)
}

// TestUpdateTask_NotFound exercises the unexported updateTask helper
// directly, covering the ErrNotFound-on-missing-row path that UpdateWith
// can no longer reach on its own (its own Get already fails first for a
// nonexistent id).
func TestUpdateTask_NotFound(t *testing.T) {
	repo := openTestRepo(t)
	_, err := updateTask(context.Background(), repo.db, todo.Task{ID: 999, Title: "x", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	require.ErrorIs(t, err, todo.ErrNotFound)
}
