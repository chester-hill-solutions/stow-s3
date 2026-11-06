package stow

import (
	"context"
	"net/http"
	"time"
)

type workspaceLifecycle uint8

// Cleanup: open -> closing -> runtime closed -> closed; failures retain their phase.
// Only open admits work. Destroy independently removes durable data (ADR 0009).
const (
	workspaceOpen workspaceLifecycle = iota
	workspaceClosing
	workspaceRuntimeClosed
	workspaceClosed
)

// Close releases the handle and session lock, making the workspace collectable.
// It preserves durable data (ADR 0009) and is idempotent.
func (w *Workspace) Close() error {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	return w.closeLocked()
}

func (w *Workspace) assertOpen() error {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if w.lifecycle != workspaceOpen {
		return ErrClosed
	}
	return nil
}

func (w *Workspace) closeRuntimeLocked() error {
	switch w.lifecycle {
	case workspaceRuntimeClosed, workspaceClosed:
		return nil
	case workspaceOpen:
		w.lifecycle = workspaceClosing
	}
	if w.facade != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := w.facade.server.Shutdown(ctx); err != nil {
			return err
		}
		done := w.facade.done
		w.facade = nil
		if err := <-done; err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	if err := w.Runtime.Close(); err != nil {
		return err
	}
	w.lifecycle = workspaceRuntimeClosed
	return nil
}

func (w *Workspace) closeLocked() error {
	if w.lifecycle == workspaceClosed {
		return nil
	}
	if err := w.closeRuntimeLocked(); err != nil {
		return err
	}
	if err := w.session.Release(); err != nil {
		return err
	}
	w.lifecycle = workspaceClosed
	return nil
}
