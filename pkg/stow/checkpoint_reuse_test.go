package stow_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestCheckpointReusesVerifiedPayloadAndSurvivesParentRemoval(t *testing.T) {
	root, registry, ws := openCheckpointTestWorkspace(t)
	writeTaskFile(t, root, "result.txt", "unchanged payload\n")
	if err := os.Chmod(filepath.Join(root, "result.txt"), 0o640); err != nil {
		t.Fatal(err)
	}
	first, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}
	second, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{ParentID: first.ID})
	if err != nil {
		t.Fatalf("second checkpoint: %v", err)
	}
	parentFile := filepath.Join(registry, "checkpoints", first.ID, "files", "result.txt")
	childFile := filepath.Join(registry, "checkpoints", second.ID, "files", "result.txt")
	parentInfo, err := os.Stat(parentFile)
	if err != nil {
		t.Fatal(err)
	}
	childInfo, err := os.Stat(childFile)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(parentInfo, childInfo) {
		t.Fatal("unchanged verified payload was copied instead of reused")
	}
	if childInfo.Mode().Perm() != 0o400 {
		t.Fatalf("stored payload mode = %o, want read-only 400", childInfo.Mode().Perm())
	}
	if err := os.RemoveAll(filepath.Dir(parentFile)); err != nil {
		t.Fatal(err)
	}
	restored, err := stow.RestoreCheckpoint(registry, second.ID, stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "restored"), RegistryDir: registry})
	if err != nil {
		t.Fatalf("restore child after parent removal: %v", err)
	}
	defer restored.Close()
	content, err := os.ReadFile(filepath.Join(restored.Dir(), "result.txt"))
	if err != nil || string(content) != "unchanged payload\n" {
		t.Fatalf("restored payload = %q, %v", content, err)
	}
	mode, err := os.Stat(filepath.Join(restored.Dir(), "result.txt"))
	if err != nil || mode.Mode().Perm() != 0o640 {
		t.Fatalf("restored source mode = %v, %v; want 640", mode, err)
	}
}

func TestCheckpointDoesNotReuseChangedSameSizePayloadWithRestoredMtime(t *testing.T) {
	root, registry, ws := openCheckpointTestWorkspace(t)
	writeTaskFile(t, root, "result.txt", "original")
	first, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}
	source := filepath.Join(root, "result.txt")
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	writeTaskFile(t, root, "result.txt", "modified")
	if err := os.Chtimes(source, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{ParentID: first.ID})
	if err != nil {
		t.Fatalf("second checkpoint: %v", err)
	}
	parentFile := filepath.Join(registry, "checkpoints", first.ID, "files", "result.txt")
	childFile := filepath.Join(registry, "checkpoints", second.ID, "files", "result.txt")
	parentInfo, err := os.Stat(parentFile)
	if err != nil {
		t.Fatal(err)
	}
	childInfo, err := os.Stat(childFile)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(parentInfo, childInfo) {
		t.Fatal("same-size content change with restored mtime reused the parent payload")
	}
	content, err := os.ReadFile(childFile)
	if err != nil || string(content) != "modified" {
		t.Fatalf("child payload = %q, %v", content, err)
	}
}

func TestCheckpointCopiesWhenParentPayloadDigestIsCorrupt(t *testing.T) {
	root, registry, ws := openCheckpointTestWorkspace(t)
	writeTaskFile(t, root, "result.txt", "source remains valid")
	first, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}
	parentFile := filepath.Join(registry, "checkpoints", first.ID, "files", "result.txt")
	if err := os.Remove(parentFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parentFile, []byte("corrupt parent bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{ParentID: first.ID})
	if err != nil {
		t.Fatalf("second checkpoint with corrupt reuse candidate: %v", err)
	}
	childFile := filepath.Join(registry, "checkpoints", second.ID, "files", "result.txt")
	parentInfo, err := os.Stat(parentFile)
	if err != nil {
		t.Fatal(err)
	}
	childInfo, err := os.Stat(childFile)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(parentInfo, childInfo) {
		t.Fatal("corrupt parent payload was reused")
	}
	content, err := os.ReadFile(childFile)
	if err != nil || string(content) != "source remains valid" {
		t.Fatalf("child payload = %q, %v", content, err)
	}
}

func TestCheckpointReuseStillChargesFullLogicalRetentionBytes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	registry := registryDir(t)
	content := "reusable payload"
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
		Dir: root, RegistryDir: registry, MaxCheckpointBytes: int64(len(content)*2 - 1),
	})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer ws.Close()
	writeTaskFile(t, root, "result.txt", content)
	first, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}
	if _, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{ParentID: first.ID}); err == nil {
		t.Fatal("unchanged hard-linked payload bypassed full logical retention accounting")
	}
	if _, err := stow.LoadCheckpoint(registry, first.ID); err != nil {
		t.Fatalf("retention rejection removed the first checkpoint: %v", err)
	}
}
