//go:build unix

package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The mechanism, tested directly.
//
// It cannot be reached through GetObject: resolveLocked already refuses a symlinked
// path, so a public-API test would pass whether the open followed or not.

// openNoFollow refuses a symlink, and says why in the error.
func TestOpenNoFollowRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(target, []byte("bytes outside"), 0o600); err != nil {
		t.Fatalf("write the target: %v", err)
	}
	link := filepath.Join(dir, "inside.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this filesystem will not hold a symlink: %v", err)
	}
	file, err := openNoFollow(link)
	if file != nil {
		file.Close()
		t.Fatal("openNoFollow opened a symlink, so a key could be swapped for a link between the resolve and the open")
	}
	if !errors.Is(err, syscall.ELOOP) {
		t.Errorf("openNoFollow on a symlink = %v, want ELOOP so the refusal names the cause", err)
	}
}

func TestOpenNoFollowOpensAnOrdinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ordinary.txt")
	if err := os.WriteFile(path, []byte("ordinary content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	file, err := openNoFollow(path)
	if err != nil {
		t.Fatalf("openNoFollow on an ordinary file: %v", err)
	}
	defer file.Close()
	body := make([]byte, len("ordinary content"))
	if _, err := file.Read(body); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "ordinary content" {
		t.Errorf("read %q", body)
	}
}

func TestOpenNoFollowRefusesADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling.txt")
	if err := os.Symlink(filepath.Join(dir, "never-created.txt"), link); err != nil {
		t.Skipf("this filesystem will not hold a symlink: %v", err)
	}
	if file, err := openNoFollow(link); err == nil {
		file.Close()
		t.Error("openNoFollow opened a dangling symlink; an ordinary open would have failed for the wrong reason")
	}
}

func TestOpenNoFollowAllowsAHardLink(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.txt")
	if err := os.WriteFile(original, []byte("shared inode"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	linked := filepath.Join(dir, "linked.txt")
	if err := os.Link(original, linked); err != nil {
		t.Skipf("this filesystem will not hold a hard link: %v", err)
	}
	file, err := openNoFollow(linked)
	if err != nil {
		t.Fatalf("openNoFollow on a hard link: %v", err)
	}
	file.Close()
}
