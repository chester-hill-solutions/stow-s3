package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// Path must not hand out a path the read paths refuse. locate follows directory
// entries and os.Stat, so a key reached through a symlinked ancestor used to
// report a path outside the workspace while GetObject refused the same key.
func TestPathRefusesASymlinkedAncestor(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	store, err := workspace.New(workspace.Options{Root: root, Bucket: "wsbucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if path, ok := store.Path("wsbucket", "link/secret.txt"); ok {
		t.Fatalf("Path returned %q, a path outside the workspace reached through a symlink", path)
	}
}
