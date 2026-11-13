package rooted

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicExistingParentDoesNotCreateDirectories(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootPath, "present"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := Open(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteAtomicExistingParent("present/file", []byte("saved"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(rootPath, "present", "file"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("published file info=%v error=%v", info, err)
	}
	if err := root.WriteAtomicExistingParent("missing/file", []byte("no"), 0o600); err == nil {
		t.Fatal("write unexpectedly created a missing parent")
	}
	if _, err := os.Lstat(filepath.Join(rootPath, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing parent was created: %v", err)
	}
}
