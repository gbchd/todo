package repository

import (
	"testing"

	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/service/todo/todotest"
)

// TestSQLiteRepository_Contract holds the SQLite adapter to the shared
// TaskRepository contract. Behaviour that any implementation of the port must
// share belongs in that suite; only assertions about SQLite itself — the
// schema version, reopening a file, the unexported helpers — stay in
// sqlite_test.go.
func TestSQLiteRepository_Contract(t *testing.T) {
	todotest.RunTaskRepositoryContract(t, func(t *testing.T) todo.TaskRepository {
		return openTestRepo(t)
	})
}
