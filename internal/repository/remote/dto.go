package remote

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/gbchd/todo/internal/service/todo"
)

// taskDTO is the host API's representation of a Task, as this client reads it.
//
// It is a second declaration of the same shape internal/transport/host writes,
// deliberately not a shared one: the two ends are two binaries that may be
// different builds, and a type they both import would make every wire change
// look source-compatible when the thing that matters is whether an already
// installed client can still read it. The protocol version header is what
// keeps them honest; a shared struct would only hide the question.
type taskDTO struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	DueDate     *string `json:"due_date"`
	ParentID    *int64  `json:"parent_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	CompletedAt *string `json:"completed_at"`

	Version int64 `json:"version"`

	ChildCount      int  `json:"child_count"`
	DoneChildCount  int  `json:"done_child_count"`
	AnyChildOverdue bool `json:"any_child_overdue"`
}

// errorBody is the JSON every host error carries. Field, Message and Conflict
// are what let this client rebuild the domain error the host raised by reading
// fields rather than parsing the prose in Error.
type errorBody struct {
	Error string `json:"error"`

	Field   string `json:"field,omitempty"`
	Message string `json:"message,omitempty"`

	Conflict *conflictBody `json:"conflict,omitempty"`
}

type conflictBody struct {
	TaskID   int64 `json:"task_id"`
	Expected int64 `json:"expected_version"`
	Actual   int64 `json:"actual_version"`
}

// toTask rebuilds a domain Task. A timestamp the host wrote and this client
// cannot parse is a broken contract, not a task with a zero date, so it is an
// error rather than a silent default.
func (d taskDTO) toTask() (todo.Task, error) {
	created, err := parseStamp(d.CreatedAt, "created_at")
	if err != nil {
		return todo.Task{}, err
	}
	updated, err := parseStamp(d.UpdatedAt, "updated_at")
	if err != nil {
		return todo.Task{}, err
	}

	t := todo.Task{
		ID:          d.ID,
		Title:       d.Title,
		Description: d.Description,
		Status:      todo.Status(d.Status),
		Priority:    todo.Priority(d.Priority),
		ParentID:    d.ParentID,
		CreatedAt:   created,
		UpdatedAt:   updated,

		Version: d.Version,

		ChildCount:      d.ChildCount,
		DoneChildCount:  d.DoneChildCount,
		AnyChildOverdue: d.AnyChildOverdue,
	}
	if d.DueDate != nil {
		due, err := time.Parse(todo.DateLayout, *d.DueDate)
		if err != nil {
			return todo.Task{}, fmt.Errorf("the host sent a due_date this todo cannot read (%q)", *d.DueDate)
		}
		t.DueDate = &due
	}
	if d.CompletedAt != nil {
		completed, err := parseStamp(*d.CompletedAt, "completed_at")
		if err != nil {
			return todo.Task{}, err
		}
		t.CompletedAt = &completed
	}
	return t, nil
}

func parseStamp(raw, field string) (time.Time, error) {
	stamp, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("the host sent a %s this todo cannot read (%q)", field, raw)
	}
	return stamp, nil
}

func toTasks(dtos []taskDTO) ([]todo.Task, error) {
	out := make([]todo.Task, 0, len(dtos))
	for _, d := range dtos {
		t, err := d.toTask()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// createBody is the POST body: only the fields a caller may choose. Status,
// the timestamps and the version are the host's, and have nowhere to arrive
// here for the same reason they are read-only on a PATCH.
type createBody struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	DueDate     *string `json:"due_date"`
	ParentID    *int64  `json:"parent_id"`
}

func toCreateBody(t todo.Task) createBody {
	return createBody{
		Title:       t.Title,
		Description: t.Description,
		Priority:    string(t.Priority),
		DueDate:     formatDate(t.DueDate),
		ParentID:    t.ParentID,
	}
}

func formatDate(d *time.Time) *string {
	if d == nil {
		return nil
	}
	s := d.Format(todo.DateLayout)
	return &s
}

// patchBody builds the PATCH body that turns before into after: one key per
// field that actually changed, and nothing else.
//
// It is a diff rather than the whole task on purpose. Sending every field
// would re-send the status the task already has, and setting a status is a
// lifecycle transition on the host — so editing the title of a done task would
// silently re-stamp its CompletedAt to now. A diff also means a mutation that
// changed nothing sends a body carrying only the precondition, which the host
// answers without touching UpdatedAt.
//
// CompletedAt is absent from the diff because it is not a field a client may
// write: it follows from Status, and the host derives it.
func patchBody(before, after todo.Task) map[string]any {
	body := map[string]any{}
	if after.Title != before.Title {
		body["title"] = after.Title
	}
	if after.Description != before.Description {
		body["description"] = after.Description
	}
	if after.Priority != before.Priority {
		body["priority"] = string(after.Priority)
	}
	if after.Status != before.Status {
		body["status"] = string(after.Status)
	}
	if !sameDate(before.DueDate, after.DueDate) {
		body["due_date"] = formatDate(after.DueDate)
	}
	if !sameParent(before.ParentID, after.ParentID) {
		body["parent_id"] = after.ParentID
	}
	return body
}

func sameDate(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func sameParent(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// filterQuery encodes a TaskFilter as the host's query string.
//
// The parent filter is three-valued and is encoded by key presence, never by
// value: an unset ParentID omits the key entirely, Set(nil) sends parent=none,
// and Set(&id) sends the id. Encoding "unconstrained" as an empty value would
// collapse the first two into the same request.
//
// status, priority and sort go out as raw strings even when they are nonsense:
// validating them here would duplicate the Service's vocabulary, and the
// Service above this port has already run.
func filterQuery(f todo.TaskFilter) url.Values {
	q := url.Values{}
	if f.Status != nil {
		q.Set("status", string(*f.Status))
	}
	if f.Priority != nil {
		q.Set("priority", string(*f.Priority))
	}
	if f.DueBefore != nil {
		q.Set("due_before", f.DueBefore.Format(todo.DateLayout))
	}
	if f.DueAfter != nil {
		q.Set("due_after", f.DueAfter.Format(todo.DateLayout))
	}
	if f.SortBy != todo.SortDefault {
		q.Set("sort", string(f.SortBy))
	}
	if f.ParentID.IsSet() {
		if parent := f.ParentID.Value(); parent == nil {
			q.Set("parent", "none")
		} else {
			q.Set("parent", strconv.FormatInt(*parent, 10))
		}
	}
	return q
}

// toDomainError rebuilds the domain error a host status and body stand for, so
// that a caller of the port cannot tell which adapter raised it. A body that
// carries no structured field falls back to the host's own message, which is
// still the most useful thing anyone has to say about it.
func toDomainError(status int, body errorBody) error {
	if body.Conflict != nil {
		return &todo.ConflictError{
			TaskID:   body.Conflict.TaskID,
			Expected: body.Conflict.Expected,
			Actual:   body.Conflict.Actual,
		}
	}
	if body.Field != "" {
		return &todo.ValidationError{Field: body.Field, Message: body.Message}
	}
	if status == 404 {
		return fmt.Errorf("%s: %w", body.Error, todo.ErrNotFound)
	}
	return errors.New(body.Error)
}
