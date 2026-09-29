package stow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func requestWorkspace(t *testing.T) (*stow.Workspace, string) {
	t.Helper()
	registry := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registry, MaxCheckpoints: 1, TTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	if err := os.WriteFile(filepath.Join(ws.Dir(), "progress.txt"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	return ws, registry
}

func TestCheckpointRequestReplaySurvivesReopenAndCapacity(t *testing.T) {
	ws, registry := requestWorkspace(t)
	request := stow.CheckpointRequest{Key: "random-persisted-request"}
	first, err := stow.CaptureCheckpoint(context.Background(), registry, ws.ID(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Outcome != "committed" {
		t.Fatalf("first = %+v", first)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "progress.txt"), []byte("later"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	replay, err := stow.CaptureCheckpoint(context.Background(), registry, ws.ID(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.Checkpoint.ID != first.Checkpoint.ID {
		t.Fatalf("replay = %+v", replay)
	}
	assertCheckpointBody(t, registry, replay.Checkpoint.ID, "progress.txt", "first")
	resolved, err := stow.ResolveCheckpoint(context.Background(), registry, ws.ID(), request)
	if err != nil || resolved.Outcome != "committed" {
		t.Fatalf("resolve = %+v, %v", resolved, err)
	}
	request.Key = "another-request"
	_, err = stow.CaptureCheckpoint(context.Background(), registry, ws.ID(), request)
	assertCheckpointCode(t, err, "capacity_exceeded")
}

func TestCheckpointRequestConflictMissingAndCorruption(t *testing.T) {
	ws, registry := requestWorkspace(t)
	request := stow.CheckpointRequest{Key: "request"}
	missing, err := stow.ResolveCheckpoint(context.Background(), registry, ws.ID(), request)
	if err != nil || missing.Outcome != "not_found" {
		t.Fatalf("missing = %+v %v", missing, err)
	}
	result, err := stow.CaptureCheckpoint(context.Background(), registry, ws.ID(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Options.MaxFiles = 5
	_, err = stow.CaptureCheckpoint(context.Background(), registry, ws.ID(), request)
	assertCheckpointCode(t, err, "request_conflict")
	request.Options.MaxFiles = 0
	if err := os.WriteFile(filepath.Join(registry, "checkpoints", result.Checkpoint.ID, "files", "progress.txt"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = stow.ResolveCheckpoint(context.Background(), registry, ws.ID(), request)
	assertCheckpointCode(t, err, "corrupt_checkpoint")
}

func TestCaptureGateCoversHandleRequestsDestroyAndCleanup(t *testing.T) {
	ws, registry := requestWorkspace(t)
	lock, err := workspace.AcquireCapture(registry, ws.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	_, err = ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if !errors.Is(err, workspace.ErrCaptureInProgress) {
		t.Fatalf("handle capture = %v", err)
	}
	_, err = stow.CaptureCheckpoint(context.Background(), registry, ws.ID(), stow.CheckpointRequest{Key: "request"})
	assertCheckpointCode(t, err, "in_progress")
	if err := ws.Destroy(context.Background()); !errors.Is(err, workspace.ErrCaptureInProgress) {
		t.Fatalf("destroy = %v", err)
	}
	if _, err := ws.PutObject(context.Background(), ws.Bucket(), "after-refusal", []byte("usable"), stow.PutOptions{}); err != nil {
		t.Fatalf("destroy contention closed live handle: %v", err)
	}
	r, err := workspace.OpenRegistryReadOnly(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ForgetCheckpoints(ws.ID()); !errors.Is(err, workspace.ErrCaptureInProgress) {
		t.Fatalf("cleanup = %v", err)
	}
	if _, err := os.Stat(ws.Dir()); err != nil {
		t.Fatal("workspace was deleted", err)
	}
}

func TestLookupWorkspaceDoesNotCreateRegistryAndRejectsTraversal(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "missing")
	if _, err := stow.LookupWorkspace(registry, "valid"); err == nil {
		t.Fatal("lookup succeeded")
	}
	if _, err := os.Stat(registry); !os.IsNotExist(err) {
		t.Fatal("lookup created registry", err)
	}
	if _, err := stow.LookupWorkspace(registry, "../escape"); err == nil {
		t.Fatal("unsafe lookup accepted")
	}
	if _, err := workspace.AcquireCapture(registry, "../escape"); err == nil {
		t.Fatal("unsafe capture accepted")
	}
}

func TestCancelledCapturePublishesNothing(t *testing.T) {
	ws, registry := requestWorkspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := stow.CheckpointRequest{Key: "cancelled"}
	_, err := stow.CaptureCheckpoint(ctx, registry, ws.ID(), request)
	assertCheckpointCode(t, err, "cancelled")
	resolved, err := stow.ResolveCheckpoint(context.Background(), registry, ws.ID(), request)
	if err != nil || resolved.Outcome != "not_found" {
		t.Fatalf("resolve = %+v %v", resolved, err)
	}
}

func assertCheckpointCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *stow.CheckpointError
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}
