package stow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointPublicationCancellationBeforeRename(t *testing.T) {
	root, stage, err := stageCheckpointDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publisher := checkpointPublisher{rename: os.Rename, syncPath: func(path string) error {
		if path == stage {
			cancel()
		}
		return syncCheckpointPath(path)
	}}
	manifest := CheckpointManifest{Version: 1, ID: "cp_000000000000000000000000", WorkspaceID: "test"}
	err = publisher.publish(ctx, stage, root, manifest)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, manifest.ID)); !os.IsNotExist(err) {
		t.Fatalf("published after cancellation: %v", err)
	}
}

func TestCheckpointPublicationSyncFailureAfterRenameIsUnknown(t *testing.T) {
	root, stage, err := stageCheckpointDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected directory sync failure")
	publisher := checkpointPublisher{rename: os.Rename, syncPath: func(path string) error {
		if path == root {
			return failure
		}
		return syncCheckpointPath(path)
	}}
	manifest := CheckpointManifest{Version: 1, ID: "cp_000000000000000000000000", WorkspaceID: "test"}
	err = publisher.publish(context.Background(), stage, root, manifest)
	var typed *CheckpointError
	if !errors.As(err, &typed) || typed.Outcome != "unknown" || !errors.Is(err, failure) {
		t.Fatalf("error = %v", err)
	}
	if _, err := LoadCheckpoint(filepath.Dir(root), manifest.ID); err != nil {
		t.Fatal("visible result should remain for reconciliation", err)
	}
}

func TestCheckpointScansAndReadsObserveCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := scanCheckpointFilesContext(ctx, root, false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := digestFileContext(ctx, filepath.Join(root, "file")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
