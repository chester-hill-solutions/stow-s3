package stow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckpointSyncFilesBoundedBeforeDirectories(t *testing.T) {
	root := t.TempDir()
	for i := range 64 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprint(i)), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release := make(chan struct{})
	var active, completed, started atomic.Int32
	publisher := checkpointPublisher{syncPath: func(path string) error {
		if path == root {
			if completed.Load() != 64 || active.Load() != 0 {
				return fmt.Errorf("directory reached before file barrier")
			}
			return nil
		}
		if active.Add(1) > 16 {
			return fmt.Errorf("unbounded synchronization")
		}
		defer active.Add(-1)
		if started.Add(1) == 2 {
			close(release)
		}
		select {
		case <-release:
			completed.Add(1)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	if err := publisher.syncTree(ctx, root); err != nil {
		t.Fatal(err)
	}
	if completed.Load() != 64 {
		t.Fatalf("completed = %d", completed.Load())
	}
}

func TestCheckpointSyncFailurePreventsPublication(t *testing.T) {
	root, stage, err := stageCheckpointDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected file flush failure")
	renamed := false
	publisher := checkpointPublisher{
		syncPath: func(string) error { return failure },
		rename: func(string, string) error {
			renamed = true
			return nil
		},
	}
	manifest := CheckpointManifest{Version: 1, ID: "cp_000000000000000000000000", WorkspaceID: "test"}
	if err := publisher.publish(context.Background(), stage, root, manifest); !errors.Is(err, failure) {
		t.Fatalf("publication error = %v", err)
	}
	if renamed {
		t.Fatal("published before successful file flush")
	}
}

func TestCheckpointSyncFilesAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	publisher := checkpointPublisher{syncPath: func(string) error {
		t.Error("unexpected sync after cancellation")
		return nil
	}}
	if err := publisher.syncFiles(ctx, []string{"file"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
