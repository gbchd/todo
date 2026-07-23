package statusverb

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func TestApply(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.Open(ctx, filepath.Join(t.TempDir(), "todo.db"))
	require.NoError(t, err, "open repo")
	t.Cleanup(func() { repo.Close() })
	svc := todo.NewService(repo)

	created, err := svc.AddTask(ctx, todo.NewTask{Title: "x"})
	require.NoError(t, err)

	got, err := Apply(ctx, svc, created.ID, todo.StatusInProgress)
	require.NoError(t, err, "Apply(in-progress)")
	assert.Equal(t, todo.StatusInProgress, got.Status)

	got, err = Apply(ctx, svc, created.ID, todo.StatusDone)
	require.NoError(t, err, "Apply(done)")
	assert.Equal(t, todo.StatusDone, got.Status)
	assert.NotNil(t, got.CompletedAt)

	got, err = Apply(ctx, svc, created.ID, todo.StatusOpen)
	require.NoError(t, err, "Apply(open)")
	assert.Equal(t, todo.StatusOpen, got.Status)
	assert.Nil(t, got.CompletedAt)
}

func TestApply_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.Open(ctx, filepath.Join(t.TempDir(), "todo.db"))
	require.NoError(t, err, "open repo")
	t.Cleanup(func() { repo.Close() })
	svc := todo.NewService(repo)

	created, _ := svc.AddTask(ctx, todo.NewTask{Title: "x"})

	_, err = Apply(ctx, svc, created.ID, todo.Status("bogus"))
	var verr *todo.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "status", verr.Field)
}
