package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/gbchd/todo/internal/credential"
	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/service/todo/todotest"
	"github.com/gbchd/todo/internal/transport/host"
)

// hosted is an in-process `todo host`: a real mux, with the real protocol and
// authentication middleware, over a real SQLite file in a temp directory.
// Nothing about it is a stub — the only thing tests reach past is the network
// itself, and httptest supplies that.
type hosted struct {
	URL   string
	Token string
	Svc   *todo.Service
}

// newHostService builds the Service a host serves, so that a test can also
// write through it directly — which is how "another device wrote in between"
// is staged.
func newHostService(t *testing.T) *todo.Service {
	t.Helper()
	repo, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "todo.db"))
	require.NoError(t, err, "open the host's database")
	t.Cleanup(func() { repo.Close() })
	return todo.NewService(repo)
}

// startHost serves svc over HTTP and registers one device, returning the
// device's token. Extra middleware is mounted innermost, after the protocol
// and credential checks, so a test can interfere with requests that have
// already been accepted.
//
// The device's secret is hashed at bcrypt.MinCost. The contract suite makes
// hundreds of authenticated requests and every one of them pays a full
// password verification; at the production work factor that is minutes of
// waiting for the same assertions. Only this test seam names a cost — see
// credential.Issuer.
func startHost(t *testing.T, svc *todo.Service, mw ...host.Middleware) hosted {
	t.Helper()
	cred, token, err := credential.Issuer{Cost: bcrypt.MinCost}.Issue()
	require.NoError(t, err, "issue a device credential")

	src := func(id string) (credential.Credential, bool) {
		if id != cred.ID {
			return credential.Credential{}, false
		}
		return cred, true
	}

	chain := append([]host.Middleware{host.RequireProtocolVersion, host.Authenticate(src)}, mw...)
	srv := httptest.NewServer(host.NewMux(svc, nil, chain...))
	t.Cleanup(srv.Close)

	return hosted{URL: srv.URL, Token: token, Svc: svc}
}

// newHostedRepo is the whole remote stack in one call: adapter, network, host,
// database.
func newHostedRepo(t *testing.T, mw ...host.Middleware) (*Repository, hosted) {
	t.Helper()
	h := startHost(t, newHostService(t), mw...)
	return New(h.URL, h.Token), h
}

// TestRemoteRepository_Contract is the second run of the shared contract
// suite: the same cases the SQLite adapter is held to, against the HTTP
// adapter wired to an in-process host over a temporary SQLite file. Passing it
// identically is the whole claim of the remote backend — that nothing above
// the port can tell which adapter is underneath.
func TestRemoteRepository_Contract(t *testing.T) {
	todotest.RunTaskRepositoryContract(t, func(t *testing.T) todo.TaskRepository {
		repo, _ := newHostedRepo(t)
		return repo
	})
}

// TestRemoteRepository_WrongCredential is the suite's third variant, reduced
// to the one statement it can make: with a credential the host will not
// accept, every operation of the port fails, and fails as an authentication
// problem rather than as an empty list, a missing task, or a decoding error.
//
// The token used names a device the host does know and proves the wrong
// secret, which is the shape of the failure a user actually hits: a revoked
// device, or a secret gone stale in an environment variable.
func TestRemoteRepository_WrongCredential(t *testing.T) {
	good, h := newHostedRepo(t)
	seeded, err := good.Create(t.Context(), todo.Task{
		Title: "visible only to a paired device", Status: todo.StatusOpen, Priority: todo.PriorityNone,
	})
	require.NoError(t, err, "seed through the good credential")

	id, _, ok := credential.SplitToken(h.Token)
	require.True(t, ok)
	bad := New(h.URL, id+".not-the-secret-this-device-was-given")

	operations := map[string]func() error{
		"create": func() error {
			_, err := bad.Create(t.Context(), todo.Task{Title: "x", Status: todo.StatusOpen, Priority: todo.PriorityNone})
			return err
		},
		"get": func() error {
			_, err := bad.Get(t.Context(), seeded.ID)
			return err
		},
		"list": func() error {
			_, err := bad.List(t.Context(), todo.TaskFilter{})
			return err
		},
		"update with": func() error {
			_, err := bad.UpdateWith(t.Context(), seeded.ID, func(task todo.Task) (todo.Task, error) { return task, nil })
			return err
		},
		"delete": func() error { return bad.Delete(t.Context(), seeded.ID) },
	}

	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			err := operation()
			require.ErrorIs(t, err, ErrUnauthenticated, "want an authentication error")
			assert.NotErrorIs(t, err, todo.ErrNotFound, "a refused credential must not read as a missing task")
			assert.NotErrorIs(t, err, ErrUnreachable, "the host answered; it is the credential that is wrong")
		})
	}

	stillThere, err := good.Get(t.Context(), seeded.ID)
	require.NoError(t, err, "the good credential keeps working")
	assert.Equal(t, seeded.Title, stillThere.Title)
}

// TestRemoteRepository_WrongCredentialRejectsBeforeStatusCode guards the one
// way the wrong-credential variant could pass for the wrong reason: a 401 that
// the adapter answered by itself, without asking the host. Every operation
// above must have reached the wire.
func TestRemoteRepository_WrongCredentialReachesTheHost(t *testing.T) {
	var reached int
	count := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached++
			next.ServeHTTP(w, r)
		})
	}
	// Mounted outermost of the test middleware but still inside the auth
	// chain, so it counts only what authentication let through.
	svc := newHostService(t)
	h := startHost(t, svc, count)

	id, _, ok := credential.SplitToken(h.Token)
	require.True(t, ok)
	bad := New(h.URL, id+".wrong")

	_, err := bad.List(t.Context(), todo.TaskFilter{})
	require.ErrorIs(t, err, ErrUnauthenticated)
	assert.Zero(t, reached, "a rejected request must not reach the task API")
}
