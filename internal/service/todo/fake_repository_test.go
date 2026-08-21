package todo

import (
	"context"
	"sort"
	"time"
)

// fakeRepository is a map-backed in-memory TaskRepository, used to unit-test
// Service in isolation from SQLite.
type fakeRepository struct {
	tasks  map[int64]Task
	nextID int64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{tasks: make(map[int64]Task)}
}

func (r *fakeRepository) Create(_ context.Context, t Task) (Task, error) {
	r.nextID++
	t.ID = r.nextID
	r.tasks[t.ID] = t
	return t, nil
}

func (r *fakeRepository) Get(_ context.Context, id int64) (Task, error) {
	t, ok := r.tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	return r.withRollup(t), nil
}

// withRollup mirrors the self-join the SQLite adapter reads through, so the
// derived fields are populated on every read here too and a Service test
// cannot pass against a rollup the real repository would never produce.
func (r *fakeRepository) withRollup(t Task) Task {
	today := time.Now()
	t.ChildCount, t.DoneChildCount, t.AnyChildOverdue = 0, 0, false
	for _, c := range r.tasks {
		if c.ParentID == nil || *c.ParentID != t.ID {
			continue
		}
		t.ChildCount++
		if c.Status == StatusDone {
			t.DoneChildCount++
			continue
		}
		if c.DueDate != nil && c.DueDate.Before(*normalizeDate(&today)) {
			t.AnyChildOverdue = true
		}
	}
	return t
}

func (r *fakeRepository) Update(_ context.Context, t Task) (Task, error) {
	if _, ok := r.tasks[t.ID]; !ok {
		return Task{}, ErrNotFound
	}
	r.tasks[t.ID] = t
	return t, nil
}

func (r *fakeRepository) UpdateWith(ctx context.Context, id int64, mutate func(Task) (Task, error)) (Task, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	updated, err := mutate(existing)
	if err != nil {
		return Task{}, err
	}
	return r.Update(ctx, updated)
}

// Delete mirrors the schema's ON DELETE CASCADE on tasks.parent_id.
func (r *fakeRepository) Delete(_ context.Context, id int64) error {
	if _, ok := r.tasks[id]; !ok {
		return ErrNotFound
	}
	delete(r.tasks, id)
	for childID, c := range r.tasks {
		if c.ParentID != nil && *c.ParentID == id {
			delete(r.tasks, childID)
		}
	}
	return nil
}

func (r *fakeRepository) List(_ context.Context, filter TaskFilter) ([]Task, error) {
	var out []Task
	for _, t := range r.tasks {
		if filter.Status != nil && t.Status != *filter.Status {
			continue
		}
		if filter.Priority != nil && t.Priority != *filter.Priority {
			continue
		}
		if filter.DueBefore != nil && (t.DueDate == nil || !t.DueDate.Before(*filter.DueBefore)) {
			continue
		}
		if filter.DueAfter != nil && (t.DueDate == nil || !t.DueDate.After(*filter.DueAfter)) {
			continue
		}
		if filter.ParentID.IsSet() {
			parent := filter.ParentID.Value()
			switch {
			case parent == nil && t.ParentID != nil:
				continue
			case parent != nil && (t.ParentID == nil || *t.ParentID != *parent):
				continue
			}
		}
		out = append(out, r.withRollup(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
