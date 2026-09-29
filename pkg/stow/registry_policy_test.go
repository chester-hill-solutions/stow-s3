package stow_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestRegistryPolicyPersistsAndCannotBeWidenedByPrepare(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	policy := stow.RegistryPolicy{Version: 1, MaxWorkspaces: 1, MaxCheckpoints: 1, MaxCheckpointBytes: 100}
	if err := stow.SetRegistryPolicy(registry, policy); err != nil {
		t.Fatal(err)
	}
	got, err := stow.GetRegistryPolicy(registry)
	if err != nil || got != policy {
		t.Fatalf("policy = %+v %v", got, err)
	}
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepare := func() (*stow.PreparedWorkspace, error) {
		return stow.PrepareWorkspace(stow.PrepareOptions{WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registry}, Inputs: []stow.WorkspaceInput{{Source: source, Destination: "file"}}, MaxWorkspaces: 100})
	}
	first, err := prepare()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Workspace.Close()
	if other, err := prepare(); err == nil {
		other.Workspace.Close()
		t.Fatal("standing cap bypassed")
	}
}

func TestRegistryAdmissionSerializesLastWorkspaceSlot(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan *stow.PreparedWorkspace, 2)
	for i := 0; i < 2; i++ {
		root := filepath.Join(t.TempDir(), "work")
		wg.Add(1)
		go func() {
			defer wg.Done()
			prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: registry}, Inputs: []stow.WorkspaceInput{{Source: source, Destination: "file"}}, MaxWorkspaces: 1})
			if err == nil {
				results <- prepared
			}
		}()
	}
	wg.Wait()
	close(results)
	count := 0
	for result := range results {
		count++
		_ = result.Workspace.Close()
	}
	if count != 1 {
		t.Fatalf("successful admissions = %d", count)
	}
}

func TestRegistryCheckpointCapsCoverCaptureImportAndDeletion(t *testing.T) {
	ws, source := requestWorkspace(t)
	ctx := context.Background()
	request := stow.CheckpointRequest{Key: "retained"}
	capture, err := stow.CaptureCheckpoint(ctx, source, ws.ID(), request)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := stow.ExportCheckpoint(ctx, source, capture.Checkpoint.ID, &archive, stow.CheckpointArchiveOptions{}); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "destination")
	if err := stow.SetRegistryPolicy(destination, stow.RegistryPolicy{Version: 1, MaxCheckpointBytes: 1}); err != nil {
		t.Fatal(err)
	}
	_, err = stow.ImportCheckpoint(ctx, destination, bytes.NewReader(archive.Bytes()), stow.CheckpointArchiveOptions{})
	assertCheckpointCode(t, err, "capacity_exceeded")
	if err := stow.SetRegistryPolicy(destination, stow.RegistryPolicy{Version: 1, MaxCheckpoints: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = stow.ImportCheckpoint(ctx, destination, bytes.NewReader(archive.Bytes()), stow.CheckpointArchiveOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := stow.DeleteCheckpoint(ctx, source, capture.Checkpoint.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err := stow.ResolveCheckpoint(ctx, source, ws.ID(), request)
	if err != nil || resolved.Outcome != "not_found" {
		t.Fatalf("after deletion %+v %v", resolved, err)
	}
}

func TestDeleteCheckpointRespectsReaderGateAndLineage(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ctx := context.Background()
	first, err := ws.CreateCheckpoint(ctx, stow.CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ws.CreateCheckpoint(ctx, stow.CheckpointOptions{ParentID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := stow.DeleteCheckpoint(ctx, registry, first.ID); err == nil {
		t.Fatal("deleted retained ancestor")
	}
	lock, err := workspace.AcquireCapture(registry, ws.ID())
	if err != nil {
		t.Fatal(err)
	}
	err = stow.DeleteCheckpoint(ctx, registry, second.ID)
	_ = lock.Release()
	if !errors.Is(err, workspace.ErrCaptureInProgress) {
		t.Fatalf("delete during read = %v", err)
	}
	if err := stow.DeleteCheckpoint(ctx, registry, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := stow.DeleteCheckpoint(ctx, registry, first.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryPolicyRejectsCorruptEntriesAndIgnoresStaging(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(filepath.Join(registry, "checkpoints", ".import-staging"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stow.SetRegistryPolicy(registry, stow.RegistryPolicy{Version: 1, MaxWorkspaces: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "corrupt.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stow.SetRegistryPolicy(registry, stow.RegistryPolicy{Version: 1, MaxWorkspaces: 2}); err == nil {
		t.Fatal("corrupt registry ignored")
	}
}
