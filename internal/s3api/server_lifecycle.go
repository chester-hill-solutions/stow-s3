package s3api

import (
	"context"
	"fmt"
	"net/http"
)

type serverLifecycle uint8

// Startup: new -> starting -> serving -> stopped (bind failure also stops).
// Shutdown: any -> stopping -> closed; failed cleanup is retryable, restart is refused.
const (
	serverNew serverLifecycle = iota
	serverStarting
	serverServing
	serverStopped
	serverStopping
	serverClosed
)

func (s *Server) beginServe() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.lifecycle {
	case serverNew:
		s.lifecycle = serverStarting
		return nil
	case serverStopping, serverClosed:
		return http.ErrServerClosed
	default:
		return fmt.Errorf("s3api: server is already started")
	}
}

func (s *Server) finishServe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.lifecycle {
	case serverStarting, serverServing:
		s.lifecycle = serverStopped
	}
	s.signalReady()
}

func (s *Server) signalReady() {
	s.readyOnce.Do(func() { close(s.ready) })
}

func (s *Server) waitForStart(ctx context.Context) error {
	// Wait for startup's owner before cleanup; completed startup wins over cancellation.
	select {
	case <-s.ready:
		return nil
	default:
	}
	select {
	case <-s.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
