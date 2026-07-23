package statusverb

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
)

func TestApply(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.Open(ctx, filepath.Join(t.TempDir(), "todo.db"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	svc := todo.NewService(repo)

	created, err := svc.AddTask(ctx, todo.NewTask{Title: "x"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	got, err := Apply(ctx, svc, created.ID, todo.StatusInProgress)
	if err != nil {
		t.Fatalf("Apply(in-progress): %v", err)
	}
	if got.Status != todo.StatusInProgress {
		t.Errorf("Status = %q, want in-progress", got.Status)
	}

	got, err = Apply(ctx, svc, created.ID, todo.StatusDone)
	if err != nil {
		t.Fatalf("Apply(done): %v", err)
	}
	if got.Status != todo.StatusDone || got.CompletedAt == nil {
		t.Errorf("got = %+v, want done with CompletedAt set", got)
	}

	got, err = Apply(ctx, svc, created.ID, todo.StatusOpen)
	if err != nil {
		t.Fatalf("Apply(open): %v", err)
	}
	if got.Status != todo.StatusOpen || got.CompletedAt != nil {
		t.Errorf("got = %+v, want open with CompletedAt cleared", got)
	}
}

func TestApply_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.Open(ctx, filepath.Join(t.TempDir(), "todo.db"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	svc := todo.NewService(repo)

	created, _ := svc.AddTask(ctx, todo.NewTask{Title: "x"})

	_, err = Apply(ctx, svc, created.ID, todo.Status("bogus"))
	var verr *todo.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want *todo.ValidationError", err)
	}
	if verr.Field != "status" {
		t.Errorf("Field = %q, want status", verr.Field)
	}
}
