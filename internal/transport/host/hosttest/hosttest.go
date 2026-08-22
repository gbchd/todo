// Package hosttest starts an in-process `todo host` for tests: the real mux,
// the real protocol and credential middleware, and a real SQLite file in a
// temporary directory. Nothing about it is a stub — the only thing a test
// reaches past is the network, and httptest supplies that.
//
// It lives in a non-test file, like the port's contract suite does, because
// three packages need the same host: the HTTP adapter's own tests, the CLI's
// end-to-end tests, and the TUI's. A host assembled three slightly different
// ways would be three slightly different claims about what a remote client
// talks to.
package hosttest

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/gbchd/todo/internal/credential"
	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/transport/host"
)

// Hosted is a running host: the URL a client points at, the token of the one
// device registered with it, and the Service it serves — which a test can also
// write through directly, to stage what another device did.
type Hosted struct {
	URL   string
	Token string
	Svc   *todo.Service
}

// NewService opens a fresh SQLite database in a temporary directory and
// returns the Service a host would serve from it.
func NewService(t *testing.T) *todo.Service {
	t.Helper()
	repo, err := repository.Open(t.Context(), filepath.Join(t.TempDir(), "host.db"))
	require.NoError(t, err, "open the host's database")
	t.Cleanup(func() { repo.Close() })
	return todo.NewService(repo)
}

// Start serves svc over HTTP and registers one device. Extra middleware is
// mounted innermost, after the protocol and credential checks, so a test can
// interfere with requests that have already been accepted.
//
// The device's secret is hashed at bcrypt.MinCost. A suite that makes hundreds
// of authenticated requests pays a full password verification on every one of
// them, which at the production work factor is minutes of waiting for the same
// assertions. This is the only place that names a cost; see credential.Issuer
// for why choosing one cannot weaken the path that issues real credentials.
func Start(t *testing.T, svc *todo.Service, mw ...host.Middleware) Hosted {
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

	return Hosted{URL: srv.URL, Token: token, Svc: svc}
}

// StartFresh is Start over a database nothing has written to yet.
func StartFresh(t *testing.T, mw ...host.Middleware) Hosted {
	t.Helper()
	return Start(t, NewService(t), mw...)
}
