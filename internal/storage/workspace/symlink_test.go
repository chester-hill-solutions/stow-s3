package workspace_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	workspace "github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// A symlink is a pointer, not content, and its target is not part of the workspace.
// Over-refusing is already covered elsewhere: TestWS03AdoptsAFileWithNoManifestEntry
// requires a nested ordinary file to be listed and readable at its true size.
//
// Adopted projects contain symlinks — a dotfile repository is mostly symlinks — so
// this is the ordinary case. Refusing one is what keeps GetObject from returning a
// target's bytes to anything able to reach the workspace's S3 surface, which
// includes the loopback endpoint. The checkpoint path refused symlinks already, so capture and
// read disagreed about what the workspace contains.
func TestASymlinkedKeyIsNotAnObject(t *testing.T) {
	const bucket = "stow-symlink-bucket"
	root, outside := workspaceWithSecretLink(t, bucket)
	store := openStore(t, root, bucket)
	defer store.Close()
	ctx := context.Background()

	listed, err := store.ListObjectsV2(ctx, bucket, storage.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, object := range listed.Objects {
		if object.Key == "notes.txt" {
			t.Errorf("list reported the symlink as an object at %d bytes, which is the length of the link and not of anything the caller asked for", object.Size)
		}
	}
	if _, err := store.HeadObject(ctx, bucket, "notes.txt"); !isAbsent(err) {
		t.Errorf("HeadObject on a symlink = %v, want the key reported as absent", err)
	}
	reader, _, err := store.GetObject(ctx, bucket, "notes.txt")
	if !isAbsent(err) {
		if reader != nil {
			reader.Close()
		}
		t.Fatalf("GetObject on a symlink = %v, want the key reported as absent", err)
	}
	if _, err := os.ReadFile(outside); err != nil {
		t.Fatalf("the refusals disturbed the file the link pointed at: %v", err)
	}
}

// TestWritingOverASymlinkReplacesItAndNotItsTarget pins the half that was already
// right, so a change to the read path cannot make the write path follow as well.
func TestWritingOverASymlinkReplacesItAndNotItsTarget(t *testing.T) {
	const bucket = "stow-symlink-bucket"
	root, outside := workspaceWithSecretLink(t, bucket)
	store := openStore(t, root, bucket)
	defer store.Close()

	secretBefore, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read the target: %v", err)
	}
	if _, err := store.PutObject(context.Background(), bucket, "notes.txt",
		bytes.NewReader([]byte("written by stow")), storage.PutOptions{}); err != nil {
		t.Fatalf("PutObject over a symlink: %v", err)
	}
	secretAfter, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("re-read the target: %v", err)
	}
	if !bytes.Equal(secretBefore, secretAfter) {
		t.Errorf("the write landed on the link's target: it is now %q, was %q", secretAfter, secretBefore)
	}
	// And the key now names a real file, which the read path will serve.
	reader, _, err := store.GetObject(context.Background(), bucket, "notes.txt")
	if err != nil {
		t.Fatalf("read back the object just written: %v", err)
	}
	defer reader.Close()
	body, _ := io.ReadAll(reader)
	if string(body) != "written by stow" {
		t.Errorf("read back %q, want the bytes that were written", body)
	}
}

// A workspace holding one symlink named for a file outside it, and that file.
func workspaceWithSecretLink(t *testing.T, bucket string) (root, outside string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "ws")
	store := openStore(t, root, bucket)
	if err := store.Close(); err != nil {
		t.Fatalf("close the store used to create the root: %v", err)
	}
	outside = filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("bytes that are not the workspace's"), 0o600); err != nil {
		t.Fatalf("write the file outside the workspace: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "notes.txt")); err != nil {
		t.Skipf("this filesystem will not hold a symlink: %v", err)
	}
	return root, outside
}

func openStore(t *testing.T, root, bucket string) *workspace.Store {
	t.Helper()
	store, err := workspace.New(workspace.Options{
		Root: root, Bucket: bucket, InitiallyOwned: true,
	})
	if err != nil {
		t.Fatalf("open the workspace at %s: %v", root, err)
	}
	return store
}

// So a test says "absent" rather than pinning whichever error the platform returned.
func isAbsent(err error) bool { return errors.Is(err, storage.ErrObjectNotFound) }
