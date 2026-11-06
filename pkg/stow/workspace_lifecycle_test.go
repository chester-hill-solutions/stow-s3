package stow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func lifecycleWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w, err := OpenWorkspace(WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func checkWorkspaceLive(t *testing.T, w *Workspace, want bool) {
	t.Helper()
	live, err := workspace.ProbeLiveness(w.Dir())
	if err != nil || live != want {
		t.Fatalf("workspace liveness = %v, %v; want %v", live, err, want)
	}
}

func TestWorkspaceFailedFacadeCloseRetainsSessionUntilRetry(t *testing.T) {
	w := lifecycleWorkspace(t)
	failure := errors.New("facade serve failed")
	server, err := s3api.New(s3api.Config{Store: storage.NewMemoryStore(), Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	// An exited facade reports its serve error after successful HTTP shutdown.
	done := make(chan error, 1)
	done <- failure
	w.facade = &workspaceServer{server: server, done: done}
	if err := w.Close(); !errors.Is(err, failure) {
		t.Fatalf("close error = %v", err)
	}
	if w.lifecycle != workspaceClosing {
		t.Fatalf("failed close phase = %v", w.lifecycle)
	}
	checkWorkspaceLive(t, w, true)
	if err := w.assertOpen(); !errors.Is(err, ErrClosed) {
		t.Fatalf("operation during close = %v", err)
	}
	if _, err := w.Facade(); !errors.Is(err, ErrClosed) {
		t.Fatalf("facade during close = %v", err)
	}
	if _, err := w.CreateCheckpoint(context.Background(), CheckpointOptions{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("checkpoint during close = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w.lifecycle != workspaceClosed {
		t.Fatalf("retried close phase = %v", w.lifecycle)
	}
	checkWorkspaceLive(t, w, false)
	if _, err := os.Stat(w.Dir()); err != nil {
		t.Fatalf("close removed durable workspace: %v", err)
	}
}

func TestWorkspaceRuntimeClosePrecedesSessionRelease(t *testing.T) {
	w := lifecycleWorkspace(t)
	w.lifecycleMu.Lock()
	err := w.closeRuntimeLocked()
	w.lifecycleMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if w.lifecycle != workspaceRuntimeClosed {
		t.Fatalf("runtime close phase = %v", w.lifecycle)
	}
	checkWorkspaceLive(t, w, true)
	if err := w.assertOpen(); !errors.Is(err, ErrClosed) {
		t.Fatalf("operation after runtime close = %v", err)
	}
	for range 2 {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	checkWorkspaceLive(t, w, false)
}
