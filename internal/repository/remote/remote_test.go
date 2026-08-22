package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/transport/host"
	"github.com/gbchd/todo/internal/transport/host/hosttest"
)

// interference tallies requests of one HTTP method reaching the task API and
// optionally answers some of them itself, which is how a lost version race, a
// stalled connection, or another device writing in between are staged
// deterministically against a real host.
type interference struct {
	method string
	seen   atomic.Int64

	// before runs on each matching request before it reaches the host; a true
	// return means the request was answered and must go no further.
	before func(w http.ResponseWriter, r *http.Request, n int64) bool
}

func (i *interference) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != i.method {
			next.ServeHTTP(w, r)
			return
		}
		n := i.seen.Add(1)
		if i.before != nil && i.before(w, r, n) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// answerConflict writes the 409 a host writes when a version precondition
// fails, naming a version nothing can match.
func answerConflict(w http.ResponseWriter, taskID int64) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]any{
		"error": "conflict",
		"conflict": map[string]any{
			"task_id": taskID, "expected_version": 1, "actual_version": 99,
		},
	})
}

func seedTask(t *testing.T, repo *Repository, title string) todo.Task {
	t.Helper()
	created, err := repo.Create(t.Context(), todo.Task{
		Title: title, Status: todo.StatusOpen, Priority: todo.PriorityNone,
	})
	require.NoError(t, err, "seed %q", title)
	return created
}

// TestUpdateWith_RetriesABoundedNumberOfTimes pins both halves of the bound: a
// mutation that loses the version race is re-run from a fresh read, and it is
// re-run at most twice before the conflict is the caller's to see.
func TestUpdateWith_RetriesABoundedNumberOfTimes(t *testing.T) {
	tests := []struct {
		name        string
		conflicts   int64
		wantErr     bool
		wantMutated int
	}{
		{"one lost race is absorbed", 1, false, 2},
		{"two lost races are absorbed", 2, false, 3},
		{"a third lost race surfaces the conflict", 3, true, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var seeded todo.Task
			block := &interference{method: http.MethodPatch}
			block.before = func(w http.ResponseWriter, _ *http.Request, n int64) bool {
				if n > tc.conflicts {
					return false
				}
				answerConflict(w, seeded.ID)
				return true
			}

			repo, _ := newHostedRepo(t, block.middleware)
			seeded = seedTask(t, repo, "contended")

			mutated := 0
			updated, err := repo.UpdateWith(t.Context(), seeded.ID, func(task todo.Task) (todo.Task, error) {
				mutated++
				task.Title = "mine"
				return task, nil
			})

			assert.Equal(t, tc.wantMutated, mutated, "times the mutation ran")
			if tc.wantErr {
				var cerr *todo.ConflictError
				require.ErrorAs(t, err, &cerr, "the conflict must reach the caller intact")
				assert.Equal(t, seeded.ID, cerr.TaskID)
				assert.Equal(t, int64(99), cerr.Actual, "the version that won must be reported")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "mine", updated.Title)
		})
	}
}

// TestUpdateWith_ReRunsAgainstAFreshRead is the reason the retry is allowed to
// exist: the mutation is a pure function of the task it is given, so re-running
// it against what the winner left behind produces a result that keeps both
// writes rather than clobbering one.
func TestUpdateWith_ReRunsAgainstAFreshRead(t *testing.T) {
	var (
		seeded todo.Task
		svc    *todo.Service
	)
	interpose := &interference{method: http.MethodPatch}
	interpose.before = func(_ http.ResponseWriter, r *http.Request, n int64) bool {
		if n == 1 {
			// Another device writes between this client's read and its write,
			// so the version it is about to name is already stale.
			_, err := svc.UpdateTask(r.Context(), seeded.ID, todo.TaskPatch{Description: todo.Set("theirs")})
			assert.NoError(t, err, "the other device's write")
		}
		return false
	}

	repo, h := newHostedRepo(t, interpose.middleware)
	svc = h.Svc
	seeded = seedTask(t, repo, "contended")

	updated, err := repo.UpdateWith(t.Context(), seeded.ID, func(task todo.Task) (todo.Task, error) {
		task.Title = "mine"
		return task, nil
	})
	require.NoError(t, err, "an ordinary edit that briefly races another device must still succeed")
	assert.Equal(t, "mine", updated.Title, "this device's write")
	assert.Equal(t, "theirs", updated.Description, "the other device's write must survive the retry")
}

// TestUpdateWith_DoesNotRetryTheMutationsOwnRefusal separates the two errors
// that look alike from the outside. A conflict raised by the host is a lost
// race worth re-running; a conflict the mutation itself raised — the Service
// checking the caller's own ExpectedVersion — is a refusal, and re-running it
// would only produce it again.
func TestUpdateWith_DoesNotRetryTheMutationsOwnRefusal(t *testing.T) {
	repo, _ := newHostedRepo(t)
	seeded := seedTask(t, repo, "orig")

	refusal := &todo.ConflictError{TaskID: seeded.ID, Expected: 7, Actual: seeded.Version}
	mutated := 0
	_, err := repo.UpdateWith(t.Context(), seeded.ID, func(task todo.Task) (todo.Task, error) {
		mutated++
		return task, refusal
	})

	require.ErrorIs(t, err, refusal)
	assert.Equal(t, 1, mutated, "a mutation that refused must not be asked again")
}

// TestCreate_IsNeverRetried holds the one operation retrying would corrupt: a
// create whose answer was lost may already have committed on the host, and a
// second attempt would leave two tasks where the user asked for one.
func TestCreate_IsNeverRetried(t *testing.T) {
	refuse := &interference{method: http.MethodPost}
	refuse.before = func(w http.ResponseWriter, _ *http.Request, _ int64) bool {
		http.Error(w, "the answer never made it back", http.StatusBadGateway)
		return true
	}
	repo, _ := newHostedRepo(t, refuse.middleware)

	_, err := repo.Create(t.Context(), todo.Task{Title: "once", Status: todo.StatusOpen, Priority: todo.PriorityNone})

	require.Error(t, err)
	assert.Equal(t, int64(1), refuse.seen.Load(), "Create must make exactly one attempt")
}

// TestRequestsCarryATimeout: a black-holed connection must fail the command,
// not wedge the terminal in front of it.
func TestRequestsCarryATimeout(t *testing.T) {
	stall := &interference{method: http.MethodGet}
	stall.before = func(_ http.ResponseWriter, r *http.Request, _ int64) bool {
		<-r.Context().Done()
		return true
	}
	h := hosttest.StartFresh(t, stall.middleware)
	repo := New(h.URL, h.Token, WithTimeout(50*time.Millisecond))

	start := time.Now()
	_, err := repo.List(t.Context(), todo.TaskFilter{})

	require.ErrorIs(t, err, ErrUnreachable)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "the deadline is the adapter's own, not the caller's")
	assert.Less(t, time.Since(start), 5*time.Second, "the request must not have waited on the server")
	assert.Contains(t, err.Error(), h.URL, "the message must name the host that did not answer")
}

// TestUnreachableHost covers the other half of the same story: nothing is
// listening at all.
func TestUnreachableHost(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()

	_, err := New(url, "id.secret").List(t.Context(), todo.TaskFilter{})

	require.ErrorIs(t, err, ErrUnreachable)
	assert.NotErrorIs(t, err, ErrUnauthenticated, "a host that never answered rejected nothing")
	assert.Contains(t, err.Error(), url)
}

// TestProtocolMismatch: the host answered, and disagrees about the protocol.
// It must not read as a bad credential or a missing task, because the fix is
// on neither of those shelves — it is an upgrade.
func TestProtocolMismatch(t *testing.T) {
	t.Run("the host rejects the version this build speaks", func(t *testing.T) {
		mangle := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set(host.ProtocolVersionHeader, "99")
				next.ServeHTTP(w, r)
			})
		}
		// Mounted outside the mux, so the protocol check sees a version this
		// build would never send — which is what an older or newer todo on
		// the other machine amounts to.
		srv := httptest.NewServer(mangle(host.NewMux(hosttest.NewService(t), nil, host.RequireProtocolVersion)))
		t.Cleanup(srv.Close)

		_, err := New(srv.URL, "id.secret").List(t.Context(), todo.TaskFilter{})

		require.ErrorIs(t, err, ErrProtocolMismatch)
		assert.Contains(t, err.Error(), "upgrade todo", "the host's own advice must reach the user")
	})

	t.Run("the host does not serve the task API at all", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(srv.Close)

		_, err := New(srv.URL, "id.secret").Get(t.Context(), 1)

		require.ErrorIs(t, err, ErrProtocolMismatch)
		assert.NotErrorIs(t, err, todo.ErrNotFound, "a route that is not served is not a task that is missing")
	})
}

// TestHostValidationErrorSurvivesTheWire is the assertion the contract suite
// cannot make on this adapter. Its validation cases drive a Service that sits
// on the client side of the port, so they are answered locally and never reach
// the host at all. This drives the port directly, with values the client-side
// Service would have refused, and asserts that the *todo.ValidationError the
// host raised arrives with its field and its message intact — matched with
// errors.As, never by comparing prose.
func TestHostValidationErrorSurvivesTheWire(t *testing.T) {
	repo, _ := newHostedRepo(t)
	parent := seedTask(t, repo, "parent")
	child, err := repo.Create(t.Context(), todo.Task{
		Title: "child", Status: todo.StatusOpen, Priority: todo.PriorityNone, ParentID: &parent.ID,
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		call        func() error
		wantField   string
		wantMessage string
	}{
		{
			name: "an empty title on create",
			call: func() error {
				_, err := repo.Create(t.Context(), todo.Task{Title: "   ", Status: todo.StatusOpen, Priority: todo.PriorityNone})
				return err
			},
			wantField:   "title",
			wantMessage: "must not be empty",
		},
		{
			name: "an invalid priority on create",
			call: func() error {
				_, err := repo.Create(t.Context(), todo.Task{Title: "x", Status: todo.StatusOpen, Priority: "urgent"})
				return err
			},
			wantField:   "priority",
			wantMessage: "invalid priority urgent",
		},
		{
			name: "a second level of nesting on create",
			call: func() error {
				_, err := repo.Create(t.Context(), todo.Task{
					Title: "grandchild", Status: todo.StatusOpen, Priority: todo.PriorityNone, ParentID: &child.ID,
				})
				return err
			},
			wantField:   "parent",
			wantMessage: "subtasks are only one level deep",
		},
		{
			name: "an empty title through update with",
			call: func() error {
				_, err := repo.UpdateWith(t.Context(), parent.ID, func(task todo.Task) (todo.Task, error) {
					task.Title = "  "
					return task, nil
				})
				return err
			},
			wantField:   "title",
			wantMessage: "must not be empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()

			var verr *todo.ValidationError
			require.ErrorAs(t, err, &verr, "a rejected field must arrive as a validation error")
			assert.Equal(t, tc.wantField, verr.Field)
			assert.Contains(t, verr.Message, tc.wantMessage)
			assert.NotErrorIs(t, err, ErrProtocolMismatch, "a rejected field is not a disagreement about the protocol")
		})
	}
}

// TestCreate_StoresANonOpenStatusInOneWrite pins both halves of what the
// create body carries. The Service never creates anything but an open task,
// but the port makes no such promise, and a repository that quietly dropped
// the status would break the contract suite's fixtures — and any future
// caller. It must also do it in the one request it is allowed: Create is never
// retried, so a second write behind it is a write whose failure would report a
// task that does exist as a task that was not created.
func TestCreate_StoresANonOpenStatusInOneWrite(t *testing.T) {
	posts := &interference{method: http.MethodPost}
	patches := &interference{method: http.MethodPatch}
	repo, _ := newHostedRepo(t, posts.middleware, patches.middleware)

	created, err := repo.Create(t.Context(), todo.Task{
		Title: "already finished", Status: todo.StatusDone, Priority: todo.PriorityNone,
	})
	require.NoError(t, err)
	assert.Equal(t, todo.StatusDone, created.Status)
	assert.NotNil(t, created.CompletedAt, "the host stamps the completion time it derives")

	assert.Equal(t, int64(1), posts.seen.Load(), "Create is one request")
	assert.Zero(t, patches.seen.Load(), "the status must not be compensated for with a second write")

	got, err := repo.Get(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, todo.StatusDone, got.Status)
}

// A dropped filter key is invisible from above the port: the Service sorts
// what it is handed a second time, so a host that never heard of the sort
// still answers in an order the caller cannot fault. The only place the key
// can be held to is the request itself.
func TestList_SendsTheWholeFilterOnTheWire(t *testing.T) {
	var query url.Values
	capture := &interference{method: http.MethodGet}
	capture.before = func(_ http.ResponseWriter, r *http.Request, _ int64) bool {
		query = r.URL.Query()
		return false
	}
	repo, _ := newHostedRepo(t, capture.middleware)

	before := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	status, priority := todo.StatusDone, todo.PriorityHigh
	_, err := repo.List(t.Context(), todo.TaskFilter{
		Status:    &status,
		Priority:  &priority,
		DueBefore: &before,
		DueAfter:  &after,
		ParentID:  todo.Set[*int64](nil),
		SortBy:    todo.SortPriority,
	})
	require.NoError(t, err)

	assert.Equal(t, "priority", query.Get("sort"), "the sort key must reach the host")
	assert.Equal(t, "done", query.Get("status"))
	assert.Equal(t, "high", query.Get("priority"))
	assert.Equal(t, "2026-08-01", query.Get("due_before"))
	assert.Equal(t, "2026-07-01", query.Get("due_after"))
	assert.Equal(t, "none", query.Get("parent"))
}

// The sort key travels as the domain spells it, because the host hands the
// raw string back to its own Service: a name this end invented would come
// back as a validation error from the other one.
func TestFilterQuery_SortKeys(t *testing.T) {
	tests := []struct {
		name string
		key  todo.SortKey
		want string
	}{
		{name: "the default is not sent at all", key: todo.SortDefault},
		{name: "priority", key: todo.SortPriority, want: "priority"},
		{name: "id", key: todo.SortID, want: "id"},
		{name: "created", key: todo.SortCreated, want: "created"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := filterQuery(todo.TaskFilter{SortBy: tt.key})
			assert.Equal(t, tt.want, q.Get("sort"))
			assert.Equal(t, tt.want != "", q.Has("sort"), "an unset sort must leave the host's own default alone")
		})
	}
}

// TestUpdateWith_LeavesCompletedAtAloneOnAnUnrelatedEdit is why the PATCH body
// is a diff rather than the whole task. Re-sending a status the task already
// has is a lifecycle transition on the host, and would silently move the
// completion time of a task nobody finished twice.
func TestUpdateWith_LeavesCompletedAtAloneOnAnUnrelatedEdit(t *testing.T) {
	repo, _ := newHostedRepo(t)
	done, err := repo.Create(t.Context(), todo.Task{
		Title: "finished", Status: todo.StatusDone, Priority: todo.PriorityNone,
	})
	require.NoError(t, err)
	require.NotNil(t, done.CompletedAt)

	edited, err := repo.UpdateWith(t.Context(), done.ID, func(task todo.Task) (todo.Task, error) {
		task.Description = "a note added later"
		return task, nil
	})
	require.NoError(t, err)
	require.NotNil(t, edited.CompletedAt)
	assert.True(t, edited.CompletedAt.Equal(*done.CompletedAt),
		"CompletedAt = %v, want the original %v", edited.CompletedAt, done.CompletedAt)
}

// classify names which of the four failures err is, by the same test every
// interface above the port uses: three sentinels and one domain type. It
// returns "" for anything that is none of them, so a case that quietly becomes
// a plain error fails loudly rather than matching by accident.
func classify(err error) string {
	switch {
	case errors.Is(err, ErrUnreachable):
		return "unreachable"
	case errors.Is(err, ErrUnauthenticated):
		return "rejected credential"
	case errors.Is(err, ErrProtocolMismatch):
		return "protocol mismatch"
	case isConflict(err):
		return "conflict"
	}
	return ""
}

// TestTheFourFailuresAreDistinguishable puts this adapter's whole error
// vocabulary in one table. Each of the four has its fix somewhere else — the
// network, the pairing, the installed version, the other device — so a caller
// must be able to tell any one of them from the other three, and the failure
// this test is written to catch is a change that collapses two of them into
// one. The three that belong to the wire also have to name the host, because
// the machine that needs fixing is not the machine reading the message.
func TestTheFourFailuresAreDistinguishable(t *testing.T) {
	tests := []struct {
		name     string
		want     string
		namesURL bool
		fail     func(t *testing.T) (url string, err error)
	}{
		{
			name: "nothing is listening", want: "unreachable", namesURL: true,
			fail: func(t *testing.T) (string, error) {
				dead := httptest.NewServer(http.NotFoundHandler())
				url := dead.URL
				dead.Close()
				_, err := New(url, "id.secret").List(t.Context(), todo.TaskFilter{})
				return url, err
			},
		},
		{
			name: "the host does not know this device", want: "rejected credential", namesURL: true,
			fail: func(t *testing.T) (string, error) {
				h := hosttest.StartFresh(t)
				_, err := New(h.URL, "someone-else.revoked").List(t.Context(), todo.TaskFilter{})
				return h.URL, err
			},
		},
		{
			name: "the two builds disagree", want: "protocol mismatch", namesURL: true,
			fail: func(t *testing.T) (string, error) {
				mangle := func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						r.Header.Set(host.ProtocolVersionHeader, "99")
						next.ServeHTTP(w, r)
					})
				}
				srv := httptest.NewServer(mangle(host.NewMux(hosttest.NewService(t), nil, host.RequireProtocolVersion)))
				t.Cleanup(srv.Close)
				_, err := New(srv.URL, "id.secret").List(t.Context(), todo.TaskFilter{})
				return srv.URL, err
			},
		},
		{
			name: "another device got there first", want: "conflict",
			fail: func(t *testing.T) (string, error) {
				race := &interference{method: http.MethodPatch}
				h := hosttest.StartFresh(t, race.middleware)
				repo := New(h.URL, h.Token)
				task := seedTask(t, repo, "contended")
				race.before = func(w http.ResponseWriter, _ *http.Request, _ int64) bool {
					answerConflict(w, task.ID)
					return true
				}
				_, err := repo.UpdateWith(t.Context(), task.ID, func(got todo.Task) (todo.Task, error) {
					got.Title = "mine"
					return got, nil
				})
				return h.URL, err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url, err := tc.fail(t)
			require.Error(t, err)
			assert.Equal(t, tc.want, classify(err), "err=%v", err)
			if tc.namesURL {
				assert.Contains(t, err.Error(), url, "the message must name the host")
			}
		})
	}
}
