// Package remote implements the todo.TaskRepository port against a `todo
// host` over HTTP. It is what a paired device uses instead of a SQLite file:
// everything above the port — the Service, the CLI, the TUI, the web UI — is
// unchanged and cannot tell which adapter is underneath.
//
// Two things about it are visible through the port, and both are documented on
// the port itself. The host's clock is authoritative, so the timestamps this
// adapter returns are not the ones it was given. And UpdateWith cannot send
// its closure over a network, so it satisfies the port's atomicity guarantee
// with a version precondition and a bounded retry instead of a transaction.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/transport/host"
)

const (
	// apiPrefix must match the host's. It is a constant rather than a value
	// read from anywhere: this build speaks one version of the protocol, and
	// the header it sends alongside says which.
	apiPrefix = "/api/v1"

	// DefaultTimeout bounds every single request. Nothing this adapter does is
	// a long poll or a stream, so a request still outstanding after this is a
	// black-holed connection, not a slow answer — and a command that hangs on
	// one is worse than a command that fails on one.
	DefaultTimeout = 15 * time.Second

	// maxResponse bounds what is read back from an address in a config file,
	// which is not necessarily a todo host at all.
	maxResponse = 4 << 20

	// maxRetries is how many times UpdateWith re-fetches and re-runs its
	// mutation after a losing a version race. Three attempts in total: enough
	// that an ordinary edit briefly racing another device succeeds anyway,
	// small enough that a task being written continuously by something else
	// surfaces as the conflict it is instead of looping.
	maxRetries = 2
)

// Repository implements todo.TaskRepository against a host.
type Repository struct {
	baseURL string
	token   string
	client  *http.Client
	timeout time.Duration

	// inFlight serializes this process's own UpdateWith calls per task id.
	//
	// The port promises that two concurrent read-modify-writes on one task
	// cannot lose one of the two. Between devices that promise is kept by the
	// precondition and the retry; within one process it is kept here, because
	// a herd of local goroutines racing each other through the network would
	// spend the retry budget on conflicts they created themselves. The map
	// only ever grows, by one small mutex per task this process has updated.
	inFlight sync.Map // int64 -> *sync.Mutex
}

// Option configures a Repository.
type Option func(*Repository)

// WithHTTPClient replaces the HTTP client requests go out on.
func WithHTTPClient(c *http.Client) Option {
	return func(r *Repository) { r.client = c }
}

// WithTimeout replaces DefaultTimeout as the bound on each request.
func WithTimeout(d time.Duration) Option {
	return func(r *Repository) { r.timeout = d }
}

// New builds a repository reading and writing the tasks of the host at
// baseURL, authenticating with token — the whole opaque string pairing handed
// this device, id and secret together.
//
// baseURL is expected to be normalized already (scheme, host, no trailing
// slash), which is what `todo pair` writes into the config file.
func New(baseURL, token string, opts ...Option) *Repository {
	r := &Repository{
		baseURL: baseURL,
		token:   token,
		client:  http.DefaultClient,
		timeout: DefaultTimeout,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Create stores a new task. It is never retried: a lost answer to a request
// the host already committed would, retried, leave two tasks where the user
// asked for one. A visible duplicate they can delete is the better failure,
// and it only happens when the network drops the reply to a write that
// succeeded.
//
// The create endpoint takes only the fields a caller may choose, so a task
// handed in already in a non-open state — which the Service never does, but a
// repository's caller may — takes a second request to put it there. That is
// also what stamps its CompletedAt, on the host's clock, like every other
// timestamp.
func (r *Repository) Create(ctx context.Context, t todo.Task) (todo.Task, error) {
	var dto taskDTO
	if err := r.do(ctx, http.MethodPost, apiPrefix+"/tasks", nil, toCreateBody(t), http.StatusCreated, &dto); err != nil {
		return todo.Task{}, err
	}
	created, err := dto.toTask()
	if err != nil {
		return todo.Task{}, err
	}
	if t.Status == "" || t.Status == created.Status {
		return created, nil
	}
	return r.patch(ctx, created.ID, map[string]any{"status": string(t.Status)})
}

// Get returns one task, or an error satisfying errors.Is(err, todo.ErrNotFound).
func (r *Repository) Get(ctx context.Context, id int64) (todo.Task, error) {
	var dto taskDTO
	if err := r.do(ctx, http.MethodGet, taskPath(id), nil, nil, http.StatusOK, &dto); err != nil {
		return todo.Task{}, err
	}
	return dto.toTask()
}

// Delete removes a task, and with it any Subtasks the host's schema cascades.
func (r *Repository) Delete(ctx context.Context, id int64) error {
	return r.do(ctx, http.MethodDelete, taskPath(id), nil, nil, http.StatusNoContent, nil)
}

// List returns the tasks matching filter. The host runs the filter through its
// own Service, so the sort and the Subtask grouping are applied there and
// again by the Service above this port; both are idempotent, and the
// duplicated work is nothing beside the round-trip that carried the answer.
func (r *Repository) List(ctx context.Context, filter todo.TaskFilter) ([]todo.Task, error) {
	var dtos []taskDTO
	if err := r.do(ctx, http.MethodGet, apiPrefix+"/tasks", filterQuery(filter), nil, http.StatusOK, &dtos); err != nil {
		return nil, err
	}
	return toTasks(dtos)
}

// UpdateWith fetches the task, applies mutate to it here, and sends the result
// back guarded by the version the fetch read. If another device wrote in that
// window the host rejects the write, and this re-fetches and re-runs mutate,
// at most maxRetries times, before surfacing the *ConflictError. That is how
// this adapter keeps the port's atomicity promise without a transaction it has
// no way to hold — and why mutate must be a pure function of the task it is
// given.
//
// An error from mutate itself is returned as it stands and never retried: the
// mutation refused this task, and handing it the same task again would only
// make it refuse it again. A *ConflictError raised by mutate — the Service
// checking a caller's own ExpectedVersion — is a refusal like any other, and
// so is likewise final.
func (r *Repository) UpdateWith(ctx context.Context, id int64, mutate func(todo.Task) (todo.Task, error)) (todo.Task, error) {
	unlock := r.lockTask(id)
	defer unlock()

	var lastConflict error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		before, err := r.Get(ctx, id)
		if err != nil {
			return todo.Task{}, err
		}
		after, err := mutate(before)
		if err != nil {
			return todo.Task{}, err
		}

		body := patchBody(before, after)
		body["expected_version"] = before.Version

		updated, err := r.patch(ctx, id, body)
		if isConflict(err) {
			lastConflict = err
			continue
		}
		return updated, err
	}
	return todo.Task{}, lastConflict
}

func isConflict(err error) bool {
	var cerr *todo.ConflictError
	return errors.As(err, &cerr)
}

func (r *Repository) patch(ctx context.Context, id int64, body map[string]any) (todo.Task, error) {
	var dto taskDTO
	if err := r.do(ctx, http.MethodPatch, taskPath(id), nil, body, http.StatusOK, &dto); err != nil {
		return todo.Task{}, err
	}
	return dto.toTask()
}

// lockTask takes this process's lock on one task id and returns its release.
func (r *Repository) lockTask(id int64) func() {
	entry, _ := r.inFlight.LoadOrStore(id, &sync.Mutex{})
	mu, _ := entry.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func taskPath(id int64) string {
	return apiPrefix + "/tasks/" + strconv.FormatInt(id, 10)
}

// do makes one request and, on the expected status, decodes the answer into
// out. Every request carries the protocol version this build speaks, this
// device's credential, and a deadline.
func (r *Repository) do(ctx context.Context, method, path string, query url.Values, body any, want int, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request for %s: %w", path, err)
		}
		payload = bytes.NewReader(encoded)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	target := r.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", path, err)
	}
	req.Header.Set(host.ProtocolVersionHeader, host.ProtocolVersion)
	req.Header.Set("Authorization", "Bearer "+r.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return r.unreachable(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != want {
		return r.errorFor(resp, path)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(out); err != nil {
		return fmt.Errorf("%w: %s answered %s with something this todo cannot read: %w",
			ErrProtocolMismatch, r.baseURL, path, err)
	}
	return nil
}

// errorFor turns an unexpected status into the error the caller should see.
//
// A 401 and an unreadable or unrouted answer are the adapter's own vocabulary;
// everything else is the host restating a domain error, which toDomainError
// rebuilds so that a caller cannot tell it crossed a network.
//
// A 400 the host did not attach a field to is read as a protocol
// disagreement, because a client whose Service has already validated the call
// has nothing else to be told: the host is rejecting the request itself — its
// version header, its encoding, a key it considers read-only — and every one
// of those means these two builds do not agree. The host's own words are
// carried through either way, so the reader sees what it actually said.
func (r *Repository) errorFor(resp *http.Response, path string) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return r.unreachable(fmt.Errorf("reading the answer to %s: %w", path, err))
	}

	var body errorBody
	if json.Unmarshal(raw, &body) != nil || body.Error == "" {
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s does not serve %s; upgrade todo on one of these machines",
				ErrProtocolMismatch, r.baseURL, path)
		}
		return fmt.Errorf("%s answered %s to %s", r.baseURL, resp.Status, path)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthenticated, body.Error)
	case resp.StatusCode == http.StatusBadRequest && body.Field == "" && body.Conflict == nil:
		return fmt.Errorf("%w: %s", ErrProtocolMismatch, body.Error)
	default:
		return toDomainError(resp.StatusCode, body)
	}
}
