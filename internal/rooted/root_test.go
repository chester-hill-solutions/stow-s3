package rooted

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootRefusesReplacedDirectory(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "root")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(path, filepath.Join(base, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.Check(); err == nil {
		t.Fatal("accepted replacement directory")
	}
	if file, err := root.OpenFile("."); err == nil {
		file.Close()
		t.Fatal("opened replacement root")
	}
}

func TestRootRefusesSymlinkAncestorsAndLeaves(t *testing.T) {
	path, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, target := range []string{outside, path} {
		link := filepath.Join(path, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Skip(err)
		}
		for _, relative := range []string{"link", "link/secret", "../secret"} {
			if file, err := root.OpenFile(relative); err == nil {
				file.Close()
				t.Fatalf("opened %q", relative)
			}
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
	}
}
