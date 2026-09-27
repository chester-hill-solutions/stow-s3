package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// putWithin runs a PutObject and fails the test if it has not returned promptly.
//
// A deadlock is not an error return, it is a goroutine that never finishes, so the
// only way to assert its absence is to put a bound on the wait. Every case here
// therefore runs the call on its own goroutine and reports the goroutine dump on
// timeout, which is the only thing that makes the failure legible: "the test timed
// out" says nothing about which lock was held twice.
func putWithin(t *testing.T, store *workspace.Store, key, body string, within time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := store.PutObject(context.Background(), bucket, key, strings.NewReader(body), storage.PutOptions{})
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(within):
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		t.Fatalf("PutObject(%q) did not return within %s; goroutine dump:\n%s", key, within, buf[:n])
		return nil
	}
}

// Putting over a file the host changed is the designed-for workspace case. Stow
// exists so an agent and stow can share a directory, so a file changing behind
// stow's back is expected rather than exceptional, and stale() exists to detect
// it. The write then has to re-derive the entry and persist it.
func TestPutObjectOverAFileTheHostChanged(t *testing.T) {
	store, root := newStore(t)

	const key = "notes.txt"
	put(t, store, key, "original")

	// Change the bytes behind stow's back, which is what makes the recorded entry
	// stale. Writing a different length guarantees the size check fires.
	//
	// The default bucket is stored at the root of the workspace, not in a
	// directory named after it, so the path here is root/key. Getting that wrong
	// writes to a file stow never looks at, and the test then passes while
	// exercising nothing.
	onDisk := filepath.Join(root, key)
	if err := os.WriteFile(onDisk, []byte("the host rewrote this, at greater length"), 0o644); err != nil {
		t.Fatalf("host write: %v", err)
	}

	if err := putWithin(t, store, key, "stow rewrites it", 10*time.Second); err != nil {
		t.Fatalf("PutObject over a host-modified file: %v", err)
	}

	if got := get(t, store, key); got != "stow rewrites it" {
		t.Fatalf("GetObject = %q, want the bytes stow wrote", got)
	}
}

// Touching a file without changing its bytes leaves the size identical, so only the
// modification time distinguishes it. This is the more common shape of the same
// hazard: an editor that saves identical content, or a tool that only sets mtime.
func TestPutObjectOverAFileTheHostOnlyTouched(t *testing.T) {
	store, root := newStore(t)

	const key = "untouched-content.txt"
	put(t, store, key, "same bytes")

	onDisk := filepath.Join(root, key)
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(onDisk, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := putWithin(t, store, key, "new bytes", 10*time.Second); err != nil {
		t.Fatalf("PutObject over a touched file: %v", err)
	}
}

// Adopting a host-created file is the other way resolve reaches record, and it
// needs no staleness at all: there is simply no manifest entry to reuse. A
// workspace is full of files stow has never seen, so this is the ordinary first
// write, not an edge case.
func TestPutObjectAdoptingAHostCreatedFile(t *testing.T) {
	store, root := newStore(t)

	const key = "created-by-the-host.txt"
	onDisk := filepath.Join(root, key)
	if err := os.WriteFile(onDisk, []byte("written before stow looked"), 0o644); err != nil {
		t.Fatalf("host write: %v", err)
	}

	if err := putWithin(t, store, key, "stow takes it over", 10*time.Second); err != nil {
		t.Fatalf("PutObject adopting a host-created file: %v", err)
	}

	if got := get(t, store, key); got != "stow takes it over" {
		t.Fatalf("GetObject = %q, want the bytes stow wrote", got)
	}
}
