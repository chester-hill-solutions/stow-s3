package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// An escaped key has to be namespaced by bucket, not just by key.
//
// A key that cannot be a path component - one with a ".." segment, one longer than
// a path may be, one that would land inside stow's own directory - is stored under
// a digest of the key. That digest did not include the bucket, so the same key in
// two buckets resolved to one physical file. A write in either bucket overwrote the
// other, a read returned the other bucket's bytes, and a delete removed an object
// belonging to a bucket the caller never named.
//
// None of that needs a contrived key. "../escape.txt" is non-natural because of the
// ".." segment, and it is the shape of key a caller reaches for when a path is
// assembled from a prefix it does not control. The natural-key path was never
// affected, which is why a workspace with only ordinary keys never showed it.
func TestEscapedKeysAreNamespacedByBucket(t *testing.T) {
	const (
		first  = "one"
		second = "two"
	)

	// Each key is non-natural for a different reason, so the test covers the whole
	// escaped route rather than one trigger. The subtest name is deliberately not
	// the key: t.TempDir builds its directory name from it, and a 300-byte key
	// exceeds the filesystem's component limit, so the first version of this failed
	// in TempDir rather than in the store.
	keys := []struct{ name, key string }{
		{"parent segment", "../escape.txt"},
		{"interior parent segment", "a/../b"},
		{"looks like internal bookkeeping", ".stow/keys/impostor"},
		{"longer than a path may be", strings.Repeat("long/", 60) + "leaf.txt"},
	}

	for _, tc := range keys {
		t.Run(tc.name, func(t *testing.T) {
			key := tc.key
			store, root := newStore(t)
			ctx := context.Background()
			for _, b := range []string{first, second} {
				if err := store.CreateBucket(ctx, b); err != nil {
					t.Fatalf("CreateBucket(%q): %v", b, err)
				}
			}

			const (
				firstBody  = "belongs to one"
				secondBody = "belongs to two"
			)
			if _, err := store.PutObject(ctx, first, key, strings.NewReader(firstBody), storage.PutOptions{}); err != nil {
				t.Fatalf("PutObject(%q, %q): %v", first, key, err)
			}
			if _, err := store.PutObject(ctx, second, key, strings.NewReader(secondBody), storage.PutOptions{}); err != nil {
				t.Fatalf("PutObject(%q, %q): %v", second, key, err)
			}

			if got := readKey(t, store, first, key); got != firstBody {
				t.Errorf("read from %q = %q, want %q - a read crossed buckets", first, got, firstBody)
			}
			if got := readKey(t, store, second, key); got != secondBody {
				t.Errorf("read from %q = %q, want %q - a read crossed buckets", second, got, secondBody)
			}

			// The two objects must be two files. One file cannot hold two bodies,
			// and this is the assertion that says so before a delete makes it moot.
			escaped := escapedFiles(t, root)
			if escaped != 2 {
				t.Errorf("%d escaped file(s) under .stow/keys, want 2 - the two buckets share one", escaped)
			}

			// Deleting in one bucket must leave the other readable. This is the
			// sharpest consequence: the caller never named the second bucket, and
			// removing its object anyway is data loss rather than a wrong answer.
			if err := store.DeleteObject(ctx, first, key); err != nil {
				t.Fatalf("DeleteObject(%q, %q): %v", first, key, err)
			}
			if got := readKey(t, store, second, key); got != secondBody {
				t.Errorf("after deleting from %q, %q reads %q, want %q - a delete crossed buckets",
					first, second, got, secondBody)
			}
			if _, _, err := store.GetObject(ctx, first, key); err == nil {
				t.Errorf("%q still readable in %q after DeleteObject", key, first)
			}
		})
	}
}

// A write in one bucket must not overwrite what another bucket holds for the same
// key, even when the second write happens first. The overwrite case is separate
// from the read case above: two correct reads are still possible if the second
// write landed on the same file and happened to match.
func TestEscapedKeyWriteInOneBucketLeavesTheOtherAlone(t *testing.T) {
	const (
		first  = "one"
		second = "two"
		key    = "../escape.txt"
	)
	store, _ := newStore(t)
	ctx := context.Background()
	for _, b := range []string{first, second} {
		if err := store.CreateBucket(ctx, b); err != nil {
			t.Fatalf("CreateBucket(%q): %v", b, err)
		}
	}

	// The second bucket is written first, so a shared file would hold its bytes
	// when the first bucket writes.
	if _, err := store.PutObject(ctx, second, key, strings.NewReader("second wrote first"), storage.PutOptions{}); err != nil {
		t.Fatalf("PutObject(%q): %v", second, err)
	}
	if _, err := store.PutObject(ctx, first, key, strings.NewReader("first wrote second"), storage.PutOptions{}); err != nil {
		t.Fatalf("PutObject(%q): %v", first, err)
	}

	if got := readKey(t, store, second, key); got != "second wrote first" {
		t.Errorf("%q in %q = %q, want its own bytes - a write crossed buckets", key, second, got)
	}
	if got := readKey(t, store, first, key); got != "first wrote second" {
		t.Errorf("%q in %q = %q, want its own bytes", key, first, got)
	}
}

// Natural keys were never affected, because their path already contains the bucket
// directory. Asserting that is what stops a fix for the escaped case from being
// taken as a reason to change the natural one.
func TestNaturalKeysRemainPerBucket(t *testing.T) {
	const (
		first  = "one"
		second = "two"
		key    = "dir/file.txt"
	)
	store, _ := newStore(t)
	ctx := context.Background()
	for _, b := range []string{first, second} {
		if err := store.CreateBucket(ctx, b); err != nil {
			t.Fatalf("CreateBucket(%q): %v", b, err)
		}
	}
	if _, err := store.PutObject(ctx, first, key, strings.NewReader("one"), storage.PutOptions{}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if _, err := store.PutObject(ctx, second, key, strings.NewReader("two"), storage.PutOptions{}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if got := readKey(t, store, first, key); got != "one" {
		t.Errorf("%s in %q = %q, want \"one\"", key, first, got)
	}
	if got := readKey(t, store, second, key); got != "two" {
		t.Errorf("%s in %q = %q, want \"two\"", key, second, got)
	}
}

func readKey(t *testing.T, store *workspace.Store, bucket, key string) string {
	t.Helper()
	body, _, err := store.GetObject(context.Background(), bucket, key)
	if err != nil {
		t.Fatalf("GetObject(%q, %q): %v", bucket, key, err)
	}
	defer body.Close()
	data := make([]byte, 4096)
	n, _ := body.Read(data)
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	return string(data[:n])
}

func escapedFiles(t *testing.T, root string) int {
	t.Helper()
	dir := filepath.Join(root, ".stow", "keys")
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return count
}
