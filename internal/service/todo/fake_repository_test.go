package todo

import (
	"context"
	"sort"
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
	return t, nil
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

func (r *fakeRepository) Delete(_ context.Context, id int64) error {
	if _, ok := r.tasks[id]; !ok {
		return ErrNotFound
	}
	delete(r.tasks, id)
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
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
