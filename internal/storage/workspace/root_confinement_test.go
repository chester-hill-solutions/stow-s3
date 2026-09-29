package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceReadsRefuseReplacedAncestorDirectory(t *testing.T) {
	root := t.TempDir()
	store := openStore(t, root, "confined")
	defer store.Close()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "directory")); err != nil {
		t.Skip(err)
	}
	if body, _, err := store.GetObject(context.Background(), "confined", "directory/secret"); err == nil {
		body.Close()
		t.Fatal("GET followed an ancestor symlink outside the workspace")
	}
	if _, err := store.HeadObject(context.Background(), "confined", "directory/secret"); err == nil {
		t.Fatal("HEAD followed an ancestor symlink outside the workspace")
	}
	if _, err := store.CopyObjectCond(context.Background(), storage.CopyRequest{SourceBucket: "confined", SourceKey: "directory/secret", DestBucket: "confined", DestKey: "copy"}); err == nil {
		t.Fatal("COPY read outside workspace")
	}
	listed, err := store.ListObjectsV2(context.Background(), "confined", storage.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Objects) != 0 {
		t.Fatalf("LIST exposed outside objects: %+v", listed.Objects)
	}
}

func TestWorkspaceRefusesReplacedRootIdentity(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	store := openStore(t, root, "confined")
	if err := os.Rename(root, filepath.Join(base, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, _, err := store.GetObject(context.Background(), "confined", "secret"); err == nil {
		body.Close()
		t.Fatal("GET accepted replaced root")
	}
	if err := store.Close(); err == nil {
		t.Fatal("Close wrote into replaced root")
	}
	if err := store.Destroy(); err == nil {
		t.Fatal("Destroy accepted replaced root")
	}
	if _, err := os.Stat(filepath.Join(root, "secret")); err != nil {
		t.Fatal(err)
	}
}
