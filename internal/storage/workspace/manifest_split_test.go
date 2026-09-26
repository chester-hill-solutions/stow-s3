package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// containsJSONKey reports whether a JSON object carries a top-level key. The
// documents are read as bytes rather than unmarshalled into a struct, because the
// assertion is about what is *absent*, and a struct cannot express that.
func containsJSONKey(raw []byte, key string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	_, ok := fields[key]
	return ok
}

// Adopting an object must not rewrite the workspace's identity document.
//
// The manifest and the object index were one file, so every adoption serialised
// and rewrote the whole thing: 20 HEADs against 20 host-written files produced 20
// full rewrites, each of a document that grows as it goes. Identity changes once
// per workspace; the index changes per object. They are separate files now, and
// this is the property that keeps them worth separating.
func TestAdoptionRewritesTheIndexAndNotTheIdentity(t *testing.T) {
	dir := t.TempDir()
	store, err := New(Options{Root: dir, Bucket: "probe"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	const files = 20
	for i := 0; i < files; i++ {
		path := filepath.Join(dir, "host", string(rune('a'+i))+".txt")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	identityBefore, err := os.Stat(manifestPath(dir))
	if err != nil {
		t.Fatalf("stat manifest: %v", err)
	}
	for i := 0; i < files; i++ {
		key := "host/" + string(rune('a'+i)) + ".txt"
		if _, err := store.HeadObject(ctx, "probe", key); err != nil {
			t.Fatalf("head %s: %v", key, err)
		}
	}
	identityAfter, err := os.Stat(manifestPath(dir))
	if err != nil {
		t.Fatalf("stat manifest: %v", err)
	}

	if !identityBefore.ModTime().Equal(identityAfter.ModTime()) {
		t.Error("adopting objects rewrote the identity document; it should change once per workspace, not per object")
	}

	raw, err := os.ReadFile(indexPath(dir))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("the index is empty after adopting 20 objects, so adoption is not being recorded")
	}
	for i := 0; i < files; i++ {
		if _, ok := store.objectIndex.entry("probe", "host/"+string(rune('a'+i))+".txt"); !ok {
			t.Fatalf("object %d is missing from the index", i)
		}
	}
}

// The two documents are genuinely separate files, so a caller inspecting a
// workspace can tell which is which and neither is a superset of the other.
func TestIdentityAndIndexAreSeparateDocuments(t *testing.T) {
	dir := t.TempDir()
	store, err := New(Options{Root: dir, Bucket: "probe"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer store.Close()

	if _, err := store.PutObject(context.Background(), "probe", "k", bytes.NewReader([]byte("body")), storage.PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	identity, err := os.ReadFile(manifestPath(dir))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	index, err := os.ReadFile(indexPath(dir))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if containsJSONKey(identity, "buckets") {
		t.Error("the identity document still carries the object index")
	}
	if !containsJSONKey(index, "buckets") {
		t.Error("the index document does not carry the object index")
	}
	if !containsJSONKey(identity, "workspace_id") {
		t.Error("the identity document does not carry the workspace identity")
	}
	if containsJSONKey(index, "workspace_id") {
		t.Error("the index document carries workspace identity, which is what this split removed")
	}
}
