package fs_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

func TestFilesystemStoreReadsLegacyLongKeyShards(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	if err := store.CreateBucket(ctx, "legacy"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	key := strings.Repeat("k", 350)
	encoded := hex.EncodeToString([]byte(key))
	segments := make([]string, 0, 6)
	for len(encoded) > 200 {
		segments = append(segments, "_"+encoded[:200])
		encoded = encoded[200:]
	}
	segments = append(segments, encoded)
	path := filepath.Join(append([]string{dir, "buckets", "legacy", "objects"}, segments...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("make legacy shard dirs: %v", err)
	}
	legacyRecord, err := json.Marshal(struct {
		Version int    `json:"version"`
		Data    []byte `json:"data"`
		ETag    string `json:"etag"`
	}{Version: 1, Data: []byte("legacy-body"), ETag: "legacy-etag"})
	if err != nil {
		t.Fatalf("marshal legacy record: %v", err)
	}
	if err := os.WriteFile(path, legacyRecord, 0o644); err != nil {
		t.Fatalf("write legacy record: %v", err)
	}

	rc, _, err := store.GetObject(ctx, "legacy", key)
	if err != nil {
		t.Fatalf("get legacy long key: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || string(got) != "legacy-body" {
		t.Fatalf("legacy body = %q, err = %v", got, err)
	}
	listed, err := store.ListObjectsV2(ctx, "legacy", storage.ListOptions{})
	if err != nil || len(listed.Objects) != 1 || listed.Objects[0].Key != key {
		t.Fatalf("legacy list = %+v, err = %v", listed, err)
	}
	if _, err := store.PutObject(ctx, "legacy", key, strings.NewReader("updated"), storage.PutOptions{}); err != nil {
		t.Fatalf("overwrite legacy long key: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("legacy record moved unexpectedly: %v", err)
	}
}

// nameMax is the portable limit on one filesystem path component. A key is hex
// encoded, so the key length that overflows a single component is half of this.
const nameMax = 255

// TestFilesystemStoreLongKeysRoundTrip covers the sizes the issue named, because
// the boundary is the whole bug: 127 bytes encodes to 254 characters and fits,
// 128 encodes to 256 and did not. Before the fix every size above 127 failed with
// ENAMETOOLONG even though ValidateKey accepts up to 1024, so the caller got a 500
// for a key it was entitled to send.
func TestFilesystemStoreLongKeysRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newContractStore(t)
	if err := store.CreateBucket(ctx, "longkeys"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	for _, size := range []int{1, 127, 128, 200, 512, 1024} {
		key := strings.Repeat("k", size)
		body := "body-" + key
		if _, err := store.PutObject(ctx, "longkeys", key, strings.NewReader(body), storage.PutOptions{}); err != nil {
			t.Fatalf("put %d-byte key: %v", size, err)
		}

		rc, meta, err := store.GetObject(ctx, "longkeys", key)
		if err != nil {
			t.Fatalf("get %d-byte key: %v", size, err)
		}
		got, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %d-byte key body: %v", size, err)
		}
		if string(got) != body {
			t.Fatalf("%d-byte key body = %d bytes, want %d", size, len(got), len(body))
		}
		if meta.Key != key {
			t.Fatalf("%d-byte key head key = %q, want the original", size, meta.Key)
		}
		if meta.Size != int64(len(body)) {
			t.Fatalf("%d-byte key size = %d, want %d", size, meta.Size, len(body))
		}
	}
}

// TestFilesystemStoreShortKeysStayFlat guards the migration claim. Only keys
// that did not fit were ever sharded, and they were never successfully written,
// so every already-stored object sits directly in the objects directory. If a
// short key ever started nesting, existing data directories would stop resolving.
func TestFilesystemStoreShortKeysStayFlat(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(ctx, "flat"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	// 127 bytes is the largest key that fits a single component unencoded.
	key := strings.Repeat("k", 127)
	if _, err := store.PutObject(ctx, "flat", key, strings.NewReader("x"), storage.PutOptions{}); err != nil {
		t.Fatalf("put 127-byte key: %v", err)
	}

	objects := filepath.Join(dir, "buckets", "flat", "objects")
	entries, err := os.ReadDir(objects)
	if err != nil {
		t.Fatalf("read objects dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("objects dir holds %d entries, want the single record: %+v", len(entries), entries)
	}
	if entries[0].IsDir() {
		t.Fatal("a key that fits one path component was written as a shard directory")
	}
}

// TestFilesystemStoreLongestKeysUseBoundedPaths proves the longest keys use a
// fixed-size path instead of a nested reversible encoding that can exceed the
// operating system's total pathname limit.
func TestFilesystemStoreLongestKeysUseBoundedPaths(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(ctx, "sharded"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	if _, err := store.PutObject(ctx, "sharded", strings.Repeat("k", 1024), strings.NewReader("x"), storage.PutOptions{}); err != nil {
		t.Fatalf("put 1024-byte key: %v", err)
	}

	objects := filepath.Join(dir, "buckets", "sharded", "objects")
	assertBoundedLayout(t, objects)
}

func assertBoundedLayout(t *testing.T, objects string) {
	t.Helper()
	var components []string
	err := filepath.Walk(objects, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == objects {
			return nil
		}
		rel, err := filepath.Rel(objects, p)
		if err != nil {
			return err
		}
		components = append(components, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk objects dir: %v", err)
	}
	if len(components) != 3 {
		t.Fatalf("1024-byte key stored as %d path component(s), want bounded digest layout: %+v", len(components), components)
	}
	for _, rel := range components {
		if filepath.IsAbs(rel) {
			continue
		}
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if len(part) > nameMax {
				t.Fatalf("path component %d chars exceeds NAME_MAX: %q", len(part), part)
			}
		}
	}
	var recordPath string
	err = filepath.Walk(objects, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			recordPath = p
		}
		return err
	})
	if err != nil {
		t.Fatalf("find object record: %v", err)
	}
	if len(recordPath) >= 1024 {
		t.Fatalf("complete record path is %d bytes, want below Darwin's pathname limit", len(recordPath))
	}
}

func TestFilesystemStoreBoundedPathsWithLongRootAndReopen(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dir := filepath.Join(base, strings.Repeat("r", 100), strings.Repeat("s", 100), strings.Repeat("t", 100), strings.Repeat("u", 100))
	store, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.CreateBucket(ctx, "bounded"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	keys := []string{strings.Repeat("a", 300), strings.Repeat("c", 512), "prefix/" + strings.Repeat("b", 1017)}
	putLongRootObjects(t, store, keys)
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	shorterDir := filepath.Join(base, "relocated")
	if err := os.Rename(dir, shorterDir); err != nil {
		t.Fatalf("move store to shorter root: %v", err)
	}
	dir = shorterDir

	store, err = fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	assertLongRootObjects(t, store, keys)
	assertLongRootPrefixAndDelete(t, store, keys)
}

func putLongRootObjects(t *testing.T, store *fs.FilesystemStore, keys []string) {
	t.Helper()
	ctx := context.Background()
	for i, key := range keys {
		body := "payload-" + string(rune('0'+i))
		if _, err := store.PutObject(ctx, "bounded", key, strings.NewReader(body), storage.PutOptions{}); err != nil {
			t.Fatalf("put %d-byte key: %v", len(key), err)
		}
	}
}

func assertLongRootObjects(t *testing.T, store *fs.FilesystemStore, keys []string) {
	t.Helper()
	ctx := context.Background()
	for i, key := range keys {
		rc, meta, err := store.GetObject(ctx, "bounded", key)
		if err != nil {
			t.Fatalf("get %d-byte key after reopen: %v", len(key), err)
		}
		got, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %d-byte key: %v", len(key), err)
		}
		if string(got) != "payload-"+string(rune('0'+i)) || meta.Key != key {
			t.Fatalf("reopened object = (%q, %q), want matching payload and key", got, meta.Key)
		}
	}
}

func assertLongRootPrefixAndDelete(t *testing.T, store *fs.FilesystemStore, keys []string) {
	t.Helper()
	ctx := context.Background()
	listed, err := store.ListObjectsV2(ctx, "bounded", storage.ListOptions{Prefix: "prefix/"})
	if err != nil {
		t.Fatalf("list by prefix after reopen: %v", err)
	}
	if len(listed.Objects) != 1 || listed.Objects[0].Key != keys[2] {
		t.Fatalf("prefix list = %+v, want the 1024-byte key", listed.Objects)
	}
	if err := store.DeleteObject(ctx, "bounded", keys[0]); err != nil {
		t.Fatalf("delete first key: %v", err)
	}
	if _, _, err := store.GetObject(ctx, "bounded", keys[0]); err == nil {
		t.Fatal("deleted bounded-path object is still readable")
	}
	if _, _, err := store.GetObject(ctx, "bounded", keys[1]); err != nil {
		t.Fatalf("get remaining key after delete: %v", err)
	}
}

func TestFilesystemStoreCompletesMultipartUploadToBoundedPath(t *testing.T) {
	ctx := context.Background()
	store := newContractStore(t)
	if err := store.CreateBucket(ctx, "multipart-bounded"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	key := strings.Repeat("m", 1024)
	upload, err := store.CreateMultipartUpload(ctx, "multipart-bounded", key, storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create multipart upload: %v", err)
	}
	part, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("multipart-body"))
	if err != nil {
		t.Fatalf("upload part: %v", err)
	}
	if _, err := store.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
		t.Fatalf("complete multipart upload: %v", err)
	}
	rc, _, err := store.GetObject(ctx, "multipart-bounded", key)
	if err != nil {
		t.Fatalf("get completed object: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || string(got) != "multipart-body" {
		t.Fatalf("completed body = %q, err = %v", got, err)
	}
	listed, err := store.ListObjectsV2(ctx, "multipart-bounded", storage.ListOptions{})
	if err != nil || len(listed.Objects) != 1 || listed.Objects[0].Key != key {
		t.Fatalf("multipart list = %+v, err = %v", listed, err)
	}
}

// TestFilesystemStoreListsMixedKeyLengths covers the sharded list walk against a
// bucket holding both layouts, and prefix filtering. The prefix case is the one
// that can silently break: shards are named after the encoded key, so filtering
// on the shard path instead of the decoded key would drop every long key whose
// shard prefix happened to disagree with the caller's prefix.
func TestFilesystemStoreListsMixedKeyLengths(t *testing.T) {
	ctx := context.Background()
	store := newContractStore(t)
	if err := store.CreateBucket(ctx, "mixed"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	shortA := "reports/2026/summary.txt"
	shortB := "reports/2026/detail.txt"
	longA := "reports/" + strings.Repeat("a", 300) + "/summary.txt"
	longB := "reports/" + strings.Repeat("b", 300) + "/summary.txt"
	keys := []string{shortA, shortB, longA, longB}
	for _, key := range keys {
		if _, err := store.PutObject(ctx, "mixed", key, strings.NewReader(key), storage.PutOptions{}); err != nil {
			t.Fatalf("put %d-byte key: %v", len(key), err)
		}
	}

	all, err := store.ListObjectsV2(ctx, "mixed", storage.ListOptions{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all.Objects) != len(keys) {
		t.Fatalf("listed %d keys, want %d: %+v", len(all.Objects), len(keys), all.Objects)
	}
	// Listing is sorted by key, so compare against the sorted set rather than
	// insertion order.
	want := append([]string(nil), keys...)
	sort.Strings(want)
	for i, key := range want {
		if all.Objects[i].Key != key {
			t.Fatalf("listed key %d = %q, want %q", i, all.Objects[i].Key, key)
		}
	}

	// A prefix that only the two long keys carry, chosen so it cannot line up
	// with the shard directory names.
	prefix := "reports/" + strings.Repeat("a", 300)
	filtered, err := store.ListObjectsV2(ctx, "mixed", storage.ListOptions{Prefix: prefix})
	if err != nil {
		t.Fatalf("list by prefix: %v", err)
	}
	if len(filtered.Objects) != 1 || filtered.Objects[0].Key != longA {
		t.Fatalf("prefix list = %+v, want just the one matching long key", filtered.Objects)
	}

	shortPrefixed, err := store.ListObjectsV2(ctx, "mixed", storage.ListOptions{Prefix: "reports/2026/"})
	if err != nil {
		t.Fatalf("list short prefix: %v", err)
	}
	if len(shortPrefixed.Objects) != 2 {
		t.Fatalf("short prefix list = %+v, want 2 keys", shortPrefixed.Objects)
	}
}
