package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestSameSecondHostEditChangesETagAndWriteCondition(t *testing.T) {
	store, old, path, info := hostEditFixture(t)
	ctx := context.Background()
	changed := info.ModTime().Truncate(time.Second).Add(500 * time.Millisecond)
	if changed.Equal(info.ModTime()) {
		changed = changed.Add(time.Nanosecond)
	}
	if err := os.WriteFile(path, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	reader, fresh, err := store.GetObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(reader)
	reader.Close()
	if readErr != nil || string(data) != "new" {
		t.Fatalf("fresh bytes=%q error=%v", data, readErr)
	}
	if fresh.ETag == old.ETag || fresh.VersionID == old.VersionID {
		t.Fatalf("host edit kept identity: old=%+v fresh=%+v", old, fresh)
	}
	if fresh.ChecksumValue != "" {
		t.Fatal("host edit retained stale checksum")
	}
	if _, err := store.PutObject(ctx, "bucket", "key", strings.NewReader("overwrite"), storage.PutOptions{IfMatch: old.ETag}); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("stale If-Match: %v", err)
	}
	if _, err := store.PutObject(ctx, "bucket", "key", strings.NewReader("accepted"), storage.PutOptions{IfMatch: fresh.ETag}); err != nil {
		t.Fatal(err)
	}
}

func hostEditFixture(t *testing.T) (*Store, *storage.ObjectMeta, string, os.FileInfo) {
	t.Helper()
	dir := t.TempDir()
	store, err := New(Options{Root: dir, Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	checksum, err := storage.ComputeChecksum("SHA256", []byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.PutObject(ctx, "bucket", "key", strings.NewReader("old"), storage.PutOptions{Metadata: map[string]string{"old": "metadata"}, ChecksumAlgorithm: "SHA256", ChecksumValue: checksum})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return store, old, path, info
}

func TestHostEditWithPreservedTimestampChangesETag(t *testing.T) {
	store, old, path, info := hostEditFixture(t)
	if err := os.WriteFile(path, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.HeadObject(context.Background(), "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ETag == old.ETag || fresh.ChecksumValue != "" {
		t.Fatalf("timestamp-preserving host edit kept stale metadata: %+v", fresh)
	}
}
