package atomicfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
)

// The platform contract for the parent-directory sync.
//
// This test is what notices if the Windows arm ever starts reporting an error
// again, and it is the only test in the repository that can fail on that platform
// for this reason: the workspace backend could not persist at all while the arm
// reported an error.
//
// It asserts the *contract* rather than the mechanism: a successful write must
// succeed, and the bytes must be there. A test that only checked "no error" would
// pass on a platform where the sync was silently skipped for the wrong reason.
func TestWriteSucceedsAndPersistsOnEveryPlatform(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "record.json")
	want := []byte(`{"version":1}`)

	if err := atomicfile.Write(path, want, 0o644); err != nil {
		t.Fatalf("write on %s: %v", runtime.GOOS, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back on %s: %v", runtime.GOOS, err)
	}
	if string(got) != string(want) {
		t.Fatalf("bytes on %s = %q, want %q", runtime.GOOS, got, want)
	}

	// The temporary file must be gone either way. A leftover is the visible
	// symptom of a rename that did not complete, and it is the one artifact a
	// caller can detect without trusting the write's own return.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read directory on %s: %v", runtime.GOOS, err)
	}
	if len(entries) != 1 || entries[0].Name() != "record.json" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory on %s holds %v, want only record.json", runtime.GOOS, names)
	}
}

// Replacing an existing file is the case the directory sync exists for, and the
// one a Windows no-op must not break. A rename over an existing destination is
// where the two platforms' durability stories differ most, so the replacement
// itself is asserted on both.
func TestReplacementSucceedsOnEveryPlatform(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")

	if err := atomicfile.Write(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("initial write on %s: %v", runtime.GOOS, err)
	}
	if err := atomicfile.Write(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("replacement write on %s: %v", runtime.GOOS, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back on %s: %v", runtime.GOOS, err)
	}
	if string(got) != "second" {
		t.Fatalf("bytes on %s = %q, want %q", runtime.GOOS, got, "second")
	}
}
