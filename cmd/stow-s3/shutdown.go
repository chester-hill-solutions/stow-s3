package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"
)

// Shutdown has its own two timeouts and one asymmetry, and it had no name, so it was
// read as part of serve rather than as a thing with a contract. Naming it makes the
// contract checkable: the outbox retry worker is given two seconds to stop before the
// server is asked to stop, and the server is given ten.
//
// The asymmetry is deliberate. A signal means the operator asked for this, so the
// process stops cleanly and says so. A serve error means the listener failed, which is
// not something to shut down gracefully around — there is nothing listening to drain —
// so it is fatal and names the error. http.ErrServerClosed is the one error the server
// raises about itself that is not a failure, because Shutdown causes it.
const (
	outboxDrainTimeout = 2 * time.Second
	shutdownTimeout    = 10 * time.Second
)

// shutdownServer is the part of a running server this package needs, so the shutdown
// path can be exercised without binding a port.
//
// *http.Server satisfies it. The interface exists for the test, and it is two methods
// rather than a mock: a mock here would be asserting against a copy of the server's
// behaviour, which is the mistake the interface is meant to avoid.
type shutdownServer interface {
	Shutdown(context.Context) error
}

// awaitShutdown blocks until the process is asked to stop or the listener fails.
//
// retryCancel and retryDone are the outbox worker's stop signal and its completion
// signal, and they are taken rather than closed over so a caller can pass a worker's
// pair from wherever it started it. Passing nil for retryDone is not supported: a
// shutdown that skipped the outbox would leave a background writer running against a
// store the server is about to close.
func awaitShutdown(srv shutdownServer, sigCh <-chan os.Signal, errCh <-chan error, retryCancel func(), retryDone <-chan struct{}) {
	select {
	case sig := <-sigCh:
		log.Printf("shutting down (%s)...", sig)
		drainOutbox(retryCancel, retryDone)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve error: %v", err)
		}
	}
}

// drainOutbox stops the retry worker and waits for it, with a bound. A worker that
// does not stop is reported rather than waited on forever: it is a background writer,
// and the server is about to close the store under it either way, so the useful
// information is that it did not stop rather than a hang.
func drainOutbox(retryCancel func(), retryDone <-chan struct{}) {
	retryCancel()
	select {
	case <-retryDone:
	case <-time.After(outboxDrainTimeout):
		log.Printf("retry worker did not stop before shutdown timeout")
	}
}
