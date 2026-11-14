package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireSessionRefusesMetadataAndLockSymlinks(t *testing.T) {
	t.Run("metadata directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "root")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, InternalPath(root)); err != nil {
			t.Fatal(err)
		}
		if _, err := AcquireSession(root); err == nil {
			t.Fatal("session accepted a symlink metadata directory")
		}
		if _, err := os.Stat(filepath.Join(outside, LockName)); !os.IsNotExist(err) {
			t.Fatalf("symlink target received lock file: %v", err)
		}
	})
	t.Run("lock file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "root")
		if err := os.MkdirAll(InternalPath(root), 0o700); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "target")
		before := []byte("do not touch")
		if err := os.WriteFile(outside, before, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(InternalPath(root), LockName)); err != nil {
			t.Fatal(err)
		}
		if _, err := AcquireSession(root); err == nil {
			t.Fatal("session accepted a symlink lock file")
		}
		after, err := os.ReadFile(outside)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("lock target changed: %q, %v", after, err)
		}
	})
}
