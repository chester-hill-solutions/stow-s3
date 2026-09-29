package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// detectContentType opens a workspace file to read its first 512 bytes, so it is a
// read of workspace content and takes the same no-follow open GetObject does. A sniff
// that crosses a link is a read that crosses a link, whatever the caller does with the
// resulting string.
//
// It cannot be reached through the public API: by the time it runs, resolve has already
// refused any link. So it is called on a link here, rather than left as a change that
// reverts silently.

// The fallback, not the target's type.
func TestDetectContentTypeRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.html")
	if err := os.WriteFile(target, []byte("<!doctype html><html><body>outside</body></html>"), 0o600); err != nil {
		t.Fatalf("write the target: %v", err)
	}
	link := filepath.Join(dir, "inside.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this filesystem will not hold a symlink: %v", err)
	}
	if got := detectContentType(link); got != "application/octet-stream" {
		t.Errorf("detectContentType on a symlink = %q, which was derived from bytes outside the workspace", got)
	}
}

func TestDetectContentTypeSniffsAnOrdinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.html")
	if err := os.WriteFile(path, []byte("<!doctype html><html><body>inside</body></html>"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := detectContentType(path); got == "application/octet-stream" {
		t.Error("detectContentType returned the fallback for an ordinary HTML file, so it is not reading")
	}
}
