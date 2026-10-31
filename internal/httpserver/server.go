package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Run serves until the listener fails or the context is canceled. Cancellation
// drains in-flight requests before returning and forces close on timeout.
func Run(ctx context.Context, server *http.Server, listener net.Listener, serve func(*http.Server, net.Listener) error, shutdownTimeout time.Duration) error {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- serve(server, listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		err := <-serveErr
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
