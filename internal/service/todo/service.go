package todo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
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

// AddTask creates a new task in StatusOpen. A non-nil input.ParentID makes it
// a Subtask of that task.
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
	if err := s.validateParent(ctx, 0, input.ParentID); err != nil {
		return Task{}, err
	}

	now := s.now()
	t := Task{
		Title:       strings.TrimSpace(input.Title),
		Description: input.Description,
		Status:      StatusOpen,
		Priority:    priority,
		DueDate:     normalizeDate(input.DueDate),
		ParentID:    input.ParentID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return s.repo.Create(ctx, t)
}

// validateParent enforces the one-level nesting rule for making taskID a
// Subtask of parentID. SQLite cannot express "the row my foreign key points at
// must itself have a NULL foreign key" without a trigger, so the rule lives
// here — and it is also what makes a cycle check unnecessary. Pass taskID 0
// for a task that does not exist yet and so cannot have children.
//
// This runs before the write rather than inside it: UpdateWith holds the
// repository's single connection for the length of its transaction, so a
// repository read from inside the mutate callback would deadlock. A parent
// deleted in the window between the two is caught by the foreign key.
func (s *Service) validateParent(ctx context.Context, taskID int64, parentID *int64) error {
	if parentID == nil {
		return nil
	}
	if *parentID == taskID {
		return &ValidationError{Field: "parent", Message: "a task cannot be its own parent"}
	}

	parent, err := s.repo.Get(ctx, *parentID)
	if errors.Is(err, ErrNotFound) {
		// Not ErrNotFound: the task being written is not the thing that is
		// missing, and letting it through would answer a bad parent_id with a
		// 404 that reads as "no such task".
		return &ValidationError{Field: "parent", Message: fmt.Sprintf("no task %d to be a subtask of", *parentID)}
	}
	if err != nil {
		return err
	}
	if parent.IsSubtask() {
		return &ValidationError{
			Field:   "parent",
			Message: fmt.Sprintf("task %d is itself a subtask; subtasks are only one level deep", parent.ID),
		}
	}

	if taskID == 0 {
		return nil
	}
	children, err := s.repo.List(ctx, TaskFilter{ParentID: Set(&taskID)})
	if err != nil {
		return err
	}
	if len(children) > 0 {
		return &ValidationError{
			Field:   "parent",
			Message: fmt.Sprintf("task %d has subtasks of its own; subtasks are only one level deep", taskID),
		}
	}
	return nil
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
	return groupUnderParents(tasks), nil
}

// groupUnderParents pulls each Subtask out of the globally sorted order and
// re-emits it directly beneath its Parent Task, preserving the sorted order
// within each group. This is why `--sort priority` with subtasks revealed is
// not a globally descending priority column. A Subtask whose parent is not in
// the result (filtered out by status, say) has nothing to group under and
// keeps its own position.
func groupUnderParents(tasks []Task) []Task {
	present := make(map[int64]bool, len(tasks))
	for _, t := range tasks {
		present[t.ID] = true
	}

	children := make(map[int64][]Task)
	for _, t := range tasks {
		if t.IsSubtask() && present[*t.ParentID] {
			children[*t.ParentID] = append(children[*t.ParentID], t)
		}
	}
	if len(children) == 0 {
		return tasks
	}

	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if t.IsSubtask() && present[*t.ParentID] {
			continue // emitted beneath its parent instead
		}
		out = append(out, t)
		out = append(out, children[t.ID]...)
	}
	return out
}

func validSortKey(k SortKey) bool {
	switch k {
	case SortDefault, SortPriority, SortID, SortCreated:
		return true
	}
	return false
}

// UpdateTask applies a partial patch; unset fields are left untouched. The
// whole patch — including a Status transition — is applied inside a single
// UpdateWith transaction, so a rejected field leaves the task untouched
// rather than partially written. Setting ParentID demotes the task to a
// Subtask; setting it to nil promotes it back to top level. Setting Status
// applies the same lifecycle rules as the explicit Start/Complete/Reopen
// verbs (see applyStatus).
func (s *Service) UpdateTask(ctx context.Context, id int64, patch TaskPatch) (Task, error) {
	if patch.ParentID.IsSet() {
		if err := s.validateParent(ctx, id, patch.ParentID.Value()); err != nil {
			return Task{}, err
		}
	}
	if patch.Status.IsSet() {
		if !validStatus(patch.Status.Value()) {
			return Task{}, &ValidationError{Field: "status", Message: "invalid status " + string(patch.Status.Value())}
		}
	}
	return s.repo.UpdateWith(ctx, id, func(existing Task) (Task, error) {
		now := s.now()
		changed := false
		if patch.Title.IsSet() {
			title := patch.Title.Value()
			if err := titleError(title); err != nil {
				return Task{}, err
			}
			existing.Title = strings.TrimSpace(title)
			changed = true
		}
		if patch.Description.IsSet() {
			existing.Description = patch.Description.Value()
			changed = true
		}
		if patch.Priority.IsSet() {
			p := patch.Priority.Value()
			if err := priorityError(p); err != nil {
				return Task{}, err
			}
			existing.Priority = p
			changed = true
		}
		if patch.DueDate.IsSet() {
			existing.DueDate = normalizeDate(patch.DueDate.Value())
			changed = true
		}
		if patch.ParentID.IsSet() {
			existing.ParentID = patch.ParentID.Value()
			changed = true
		}
		if patch.Status.IsSet() {
			existing = applyStatus(existing, patch.Status.Value(), now)
			changed = true
		}
		if changed {
			existing.UpdatedAt = now
		}
		return existing, nil
	})
}

// StartTask transitions a task to StatusInProgress.
func (s *Service) StartTask(ctx context.Context, id int64) (Task, error) {
	return s.repo.UpdateWith(ctx, id, func(existing Task) (Task, error) {
		now := s.now()
		existing = applyStatus(existing, StatusInProgress, now)
		existing.UpdatedAt = now
		return existing, nil
	})
}

// CompleteTask transitions a task to StatusDone and stamps CompletedAt.
func (s *Service) CompleteTask(ctx context.Context, id int64) (Task, error) {
	return s.repo.UpdateWith(ctx, id, func(existing Task) (Task, error) {
		now := s.now()
		existing = applyStatus(existing, StatusDone, now)
		existing.UpdatedAt = now
		return existing, nil
	})
}

// ReopenTask transitions a task back to StatusOpen, clearing CompletedAt.
func (s *Service) ReopenTask(ctx context.Context, id int64) (Task, error) {
	return s.repo.UpdateWith(ctx, id, func(existing Task) (Task, error) {
		now := s.now()
		existing = applyStatus(existing, StatusOpen, now)
		existing.UpdatedAt = now
		return existing, nil
	})
}

// applyStatus transitions t to target, applying the lifecycle's CompletedAt
// rule exactly once: done stamps CompletedAt to now, open and in-progress
// clear it. Shared by UpdateTask and the Start/Complete/Reopen verbs so the
// rule lives in one place.
func applyStatus(t Task, target Status, now time.Time) Task {
	t.Status = target
	if target == StatusDone {
		t.CompletedAt = &now
	} else {
		t.CompletedAt = nil
	}
	return t
}

// DeleteTask permanently removes a task, and with it any Subtasks it has
// (the schema cascades).
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
	slices.SortStableFunc(tasks, func(a, b Task) int {
		switch sortBy {
		case SortPriority:
			return cmp.Compare(priorityRank[b.Priority], priorityRank[a.Priority])
		case SortID:
			return cmp.Compare(a.ID, b.ID)
		case SortCreated:
			return a.CreatedAt.Compare(b.CreatedAt)
		default:
			switch {
			case defaultLess(a, b):
				return -1
			case defaultLess(b, a):
				return 1
			default:
				return 0
			}
		}
	})
}
