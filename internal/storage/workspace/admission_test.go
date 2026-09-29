package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestRegistryLookupRejectsMismatchedAndUnsafeIdentity(t *testing.T) {
	registry := registryIn(t)
	if err := registry.Register(workspace.Entry{ID: "actual"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(registry.Dir(), "actual.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry.Dir(), "requested.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Lookup("requested"); err == nil {
		t.Fatal("mismatched identity accepted")
	}
	if _, _, err := registry.Lookup("../actual"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := registry.Register(workspace.Entry{ID: "../escape"}); err == nil {
		t.Fatal("unsafe registration accepted")
	}
	if _, err := registry.AllStrict(); err == nil {
		t.Fatal("corrupt identity ignored by accounting")
	}
}

func TestCollectAndPruneRespectActiveCapture(t *testing.T) {
	registry := registryIn(t)
	id, root := registerOwned(t, registry, time.Second)
	lock, err := workspace.AcquireCapture(registry.Dir(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	results, err := registry.Collect(expired())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reasons(results)[id], "capture") {
		t.Fatalf("collect results = %+v", results)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("capture workspace was removed", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	results, err = registry.Prune(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reasons(results)[id], "capture") {
		t.Fatalf("prune results = %+v", results)
	}
	if _, found, err := registry.Lookup(id); err != nil || !found {
		t.Fatalf("active capture identity forgotten: %v", err)
	}
}

func TestCaptureLockFailsClosedOnInvalidLockDirectory(t *testing.T) {
	registry := t.TempDir()
	if err := os.WriteFile(filepath.Join(registry, "capture-locks"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := workspace.AcquireCapture(registry, "workspace")
	if err == nil || lock != nil {
		t.Fatalf("lock = %v, error = %v", lock, err)
	}
}
