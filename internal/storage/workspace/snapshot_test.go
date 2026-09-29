package workspace_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestLogicalSnapshotPreservesExactNaturalAndEscapedKeyIdentity(t *testing.T) {
	store, root := newStore(t)
	put(t, store, "Report.txt", "upper")
	put(t, store, "report.txt", "lower")
	put(t, store, "odd?/key", "escaped")
	if err := os.WriteFile(filepath.Join(root, "empty"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Host-written files have no index record and still appear with their exact spelling.
	if err := os.WriteFile(filepath.Join(root, "Host.txt"), []byte("host"), 0600); err != nil {
		t.Fatal(err)
	}
	// An indexed file removed by the host must not reappear as an object.
	put(t, store, "deleted.txt", "deleted")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := workspace.ReadLogicalSnapshot(context.Background(), root, workspace.SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Report.txt": "upper", "report.txt": "lower", "odd?/key": "escaped", "Host.txt": "host", "empty": ""}
	if len(snapshot.Objects) != len(want) {
		t.Fatalf("objects=%+v", snapshot.Objects)
	}
	for _, object := range snapshot.Objects {
		if object.Key == "empty" && object.ContentType != "application/octet-stream" {
			t.Fatalf("empty content type = %s", object.ContentType)
		}
		content, exists := want[object.Key]
		if !exists {
			t.Fatalf("unexpected object %s", object.Key)
		}
		sum := sha256.Sum256([]byte(content))
		if object.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("wrong content for %s", object.Key)
		}
	}
}

func TestLogicalSnapshotRefusesLinkedInternalDirectory(t *testing.T) {
	store, root := newStore(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, ".stow")
	moved := filepath.Join(t.TempDir(), "private")
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, original); err != nil {
		t.Skip(err)
	}
	if _, err := workspace.ReadLogicalSnapshot(context.Background(), root, workspace.SnapshotOptions{}); err == nil {
		t.Fatal("snapshot read linked internal documents")
	}
	// Restore for the store cleanup hook.
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, original); err != nil {
		t.Fatal(err)
	}
}
