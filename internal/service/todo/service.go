package todo

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Service is the hexagon: domain logic on top of a TaskRepository port.
type Service struct {
	repo TaskRepository
	now  func() time.Time
}

// NewService wires a Service to the given repository.
func NewService(repo TaskRepository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func titleError(title string) error {
	if strings.TrimSpace(title) == "" {
		return &ValidationError{Field: "title", Message: "must not be empty"}
	}
	return nil
}

func priorityError(p Priority) error {
	if !validPriority(p) {
		return &ValidationError{Field: "priority", Message: "invalid priority " + string(p)}
	}
	return nil
}

// AddTask creates a new task in StatusOpen.
func (s *Service) AddTask(ctx context.Context, input NewTask) (Task, error) {
	if err := titleError(input.Title); err != nil {
		return Task{}, err
	}
	priority := input.Priority
	if priority == "" {
		priority = PriorityNone
	}
	if err := priorityError(priority); err != nil {
		return Task{}, err
	}

	now := s.now()
	t := Task{
		Title:       strings.TrimSpace(input.Title),
		Description: input.Description,
		Status:      StatusOpen,
		Priority:    priority,
		DueDate:     normalizeDate(input.DueDate),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return s.repo.Create(ctx, t)
}

// GetTask returns a single task by id, or ErrNotFound.
func (s *Service) GetTask(ctx context.Context, id int64) (Task, error) {
	return s.repo.Get(ctx, id)
}

// ListTasks returns tasks matching filter, sorted per filter.SortBy.
func (s *Service) ListTasks(ctx context.Context, filter TaskFilter) ([]Task, error) {
	if filter.Status != nil && !validStatus(*filter.Status) {
		return nil, &ValidationError{Field: "status", Message: "invalid status " + string(*filter.Status)}
	}
	if filter.Priority != nil {
		if err := priorityError(*filter.Priority); err != nil {
			return nil, err
		}
	}
	if !validSortKey(filter.SortBy) {
		return nil, &ValidationError{Field: "sort", Message: "invalid sort " + string(filter.SortBy)}
	}

	tasks, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	sortTasks(tasks, filter.SortBy)
	return tasks, nil
}

func validSortKey(k SortKey) bool {
	switch k {
	case SortDefault, SortPriority, SortID, SortCreated:
		return true
	}
	return false
}

// UpdateTask applies a partial patch; unset fields are left untouched.
func (s *Service) UpdateTask(ctx context.Context, id int64, patch TaskPatch) (Task, error) {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}

	if patch.Title.IsSet() {
		title := patch.Title.Value()
		if err := titleError(title); err != nil {
			return Task{}, err
		}
		existing.Title = strings.TrimSpace(title)
	}
	if patch.Description.IsSet() {
		existing.Description = patch.Description.Value()
	}
	if patch.Priority.IsSet() {
		p := patch.Priority.Value()
		if err := priorityError(p); err != nil {
			return Task{}, err
		}
		existing.Priority = p
	}
	if patch.DueDate.IsSet() {
		existing.DueDate = normalizeDate(patch.DueDate.Value())
	}
	existing.UpdatedAt = s.now()

	return s.repo.Update(ctx, existing)
}

// StartTask transitions a task to StatusInProgress.
func (s *Service) StartTask(ctx context.Context, id int64) (Task, error) {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	existing.Status = StatusInProgress
	existing.CompletedAt = nil
	existing.UpdatedAt = s.now()
	return s.repo.Update(ctx, existing)
}

// CompleteTask transitions a task to StatusDone and stamps CompletedAt.
func (s *Service) CompleteTask(ctx context.Context, id int64) (Task, error) {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	now := s.now()
	existing.Status = StatusDone
	existing.CompletedAt = &now
	existing.UpdatedAt = now
	return s.repo.Update(ctx, existing)
}

// ReopenTask transitions a task back to StatusOpen, clearing CompletedAt.
func (s *Service) ReopenTask(ctx context.Context, id int64) (Task, error) {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	existing.Status = StatusOpen
	existing.CompletedAt = nil
	existing.UpdatedAt = s.now()
	return s.repo.Update(ctx, existing)
}

// DeleteTask permanently removes a task.
func (s *Service) DeleteTask(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func statusRank(s Status) int {
	switch s {
	case StatusInProgress:
		return 0
	case StatusOpen:
		return 1
	default:
		return 2
	}
}

func defaultLess(a, b Task) bool {
	if ra, rb := statusRank(a.Status), statusRank(b.Status); ra != rb {
		return ra < rb
	}
	switch {
	case a.DueDate == nil && b.DueDate != nil:
		return false
	case a.DueDate != nil && b.DueDate == nil:
		return true
	case a.DueDate != nil && b.DueDate != nil && !a.DueDate.Equal(*b.DueDate):
		return a.DueDate.Before(*b.DueDate)
	default:
		return priorityRank[a.Priority] > priorityRank[b.Priority]
	}
}

func sortTasks(tasks []Task, sortBy SortKey) {
	sort.SliceStable(tasks, func(i, j int) bool {
		switch sortBy {
		case SortPriority:
			return priorityRank[tasks[i].Priority] > priorityRank[tasks[j].Priority]
		case SortID:
			return tasks[i].ID < tasks[j].ID
		case SortCreated:
			return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
		default:
			return defaultLess(tasks[i], tasks[j])
		}
	})
}
