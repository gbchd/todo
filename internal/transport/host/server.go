package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gbchd/todo/internal/credential"
	"github.com/gbchd/todo/internal/pairing"
	"github.com/gbchd/todo/internal/service/todo"
)

// Run starts the host bound to addr and blocks until ctx is canceled or the
// server fails. creds is consulted on every request to resolve the device a
// token names, and pairs is the outstanding pairing offer that `todo host
// pair` opens in another process.
//
// Whether addr is safe to bind is settled before this is called: see
// config.HostConfig.Validate, which refuses a non-loopback address while no
// device credentials are registered. Run is given an address it may listen on.
func Run(ctx context.Context, svc *todo.Service, addr string, creds credential.Source, pairs *pairing.Store, stdout io.Writer) error {
	server := &http.Server{
		Addr: addr,
		// Order is outermost first: the protocol version is checked before the
		// credential, so a client too old to be understood is told to upgrade
		// rather than told its credential is bad.
		Handler:           NewMux(svc, pairs, RequireProtocolVersion, Authenticate(creds)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	fmt.Fprintf(stdout, "Hosting the task API on http://%s%s\n", addr, apiPrefix)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx) //nolint:contextcheck // context is used for timeout, not cancellation
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
