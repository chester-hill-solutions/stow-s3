// A symlinked escaped key.
//
// Escaped keys are too long for the reversible path encoding, so they live under the
// internal directory, the tree walk cannot see them, and they come from the object
// index — where the existence check used os.Stat, which follows. A key written through
// the store and then replaced with a link was listed at the size of the link, and the
// content-type sniff behind the listing opened the target.
//
// It has to be written through the store to reach that loop: a file dropped on disk by
// hand is skipped for a different reason, which made the first version pass either way.
package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	workspace "github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestASymlinkedEscapedKeyIsNotAnObject(t *testing.T) {
	const bucket = "stow-escaped-link-bucket"
	root := filepath.Join(t.TempDir(), "ws")
	store, err := workspace.New(workspace.Options{Root: root, Bucket: bucket, InitiallyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("SECRET-BYTES-NOT-IN-THE-WORKSPACE"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A key long enough that the reversible path encoding cannot be used, which is
	// what puts it under the internal directory and on the escaped-key path. It has
	// to be written through the store so the object index knows about it — a file
	// dropped on disk by hand is skipped for that reason, not because of the link.
	key := "kk/"
	for len(key) < 700 {
		key += "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	}
	if _, err := store.PutObject(context.Background(), bucket, key,
		strings.NewReader("ordinary content"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	escaped := workspace.EscapedPath(root, bucket, key)
	if err := os.Remove(escaped); err != nil {
		t.Fatalf("remove the written file: %v", err)
	}
	if err := os.Symlink(outside, escaped); err != nil {
		t.Skipf("no symlink: %v", err)
	}
	listed, err := store.ListObjectsV2(context.Background(), bucket, storage.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Objects) != 0 {
		t.Errorf("list reported %d object(s) for a key whose file is a symlink, the first at %d bytes — the length of the link, not of anything in the workspace",
			len(listed.Objects), listed.Objects[0].Size)
	}
	// And the read, which is the exposure: the content-type sniff behind the listing
	// opens the file, so a listed key meant bytes were being read from outside.
	if _, _, err := store.GetObject(context.Background(), bucket, key); err == nil {
		t.Error("GetObject served a key whose file is a symlink")
	}
}
