package atomicfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
)

func rootWriteFixture(t *testing.T) (string, *os.Root, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("workspace rooted mutation profile is unsupported on Windows")
	}
	dir, outside := t.TempDir(), t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir, root, secret
}

func TestRootWriteReplacesLeafWithoutFollowingIt(t *testing.T) {
	dir, root, secret := rootWriteFixture(t)
	if err := os.Symlink(secret, filepath.Join(dir, "leaf")); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"first", "replacement"} {
		if err := atomicfile.WriteRoot(root, "leaf", []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := root.ReadFile("leaf")
		if err != nil || string(got) != data {
			t.Fatalf("write bytes=%q err=%v", got, err)
		}
	}
	got, err := os.ReadFile(secret)
	if err != nil || string(got) != "outside" {
		t.Fatalf("leaf target changed: %q %v", got, err)
	}
}

func TestRootWriteFailureCleansTemporaryFile(t *testing.T) {
	dir, root, secret := rootWriteFixture(t)
	if err := os.Mkdir(filepath.Join(dir, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteRoot(root, "directory", []byte("cannot replace directory"), 0600); err == nil {
		t.Fatal("replaced directory")
	}
	if err := os.Symlink(filepath.Dir(secret), filepath.Join(dir, "ancestor")); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteRoot(root, "ancestor/secret", []byte("escaped"), 0600); err == nil {
		t.Fatal("followed outside ancestor")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("failed write left temp files: %+v", entries)
	}
	got, err := os.ReadFile(secret)
	if err != nil || string(got) != "outside" {
		t.Fatalf("ancestor target changed: %q %v", got, err)
	}
}
