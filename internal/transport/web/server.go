package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gbchd/todo/internal/service/todo"
)

// Run starts the web adapter bound to addr (expected to be a 127.0.0.1
// address per the design's no-auth, single-machine constraint) and blocks
// until ctx is canceled or the server fails.
func Run(ctx context.Context, svc *todo.Service, addr string, stdout io.Writer) error {
	server := &http.Server{Addr: addr, Handler: NewMux(svc)}

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	fmt.Fprintf(stdout, "Serving on http://%s\n", addr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
