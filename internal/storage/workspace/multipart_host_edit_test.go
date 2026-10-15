package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const multipartHostBucket = "multipart-host"

func TestMultipartHostEditInvalidatesIdentity(t *testing.T) {
	for _, parts := range []int{1, 2} {
		for _, reopen := range []bool{false, true} {
			for _, preserveTime := range []bool{false, true} {
				t.Run(fmt.Sprintf("parts=%d/reopen=%v/preserveTime=%v", parts, reopen, preserveTime), func(t *testing.T) {
					store, root, old := multipartHostFixture(t, parts)
					if reopen {
						store = reopenMultipartStore(t, store, root)
					}
					unchanged, err := store.HeadObject(context.Background(), multipartHostBucket, "artifact")
					if err != nil || unchanged.ETag != old.ETag {
						t.Fatalf("unchanged identity: %+v %v", unchanged, err)
					}
					path := filepath.Join(root, "artifact")
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					file, err := os.OpenFile(path, os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, err = file.WriteAt([]byte("new"), 0)
					closeErr := file.Close()
					if err != nil || closeErr != nil {
						t.Fatalf("edit: %v %v", err, closeErr)
					}
					modified := info.ModTime()
					if !preserveTime {
						modified = modified.Truncate(time.Second).Add(500 * time.Millisecond)
					}
					if err := os.Chtimes(path, modified, modified); err != nil {
						t.Fatal(err)
					}
					assertMultipartHostEdit(t, store, old)
				})
			}
		}
	}
}

func assertMultipartHostEdit(t *testing.T, store *Store, old *storage.ObjectMeta) {
	t.Helper()
	ctx := context.Background()
	reader, fresh, err := store.GetObject(ctx, multipartHostBucket, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(reader)
	reader.Close()
	if readErr != nil || !bytes.HasPrefix(data, []byte("new")) {
		t.Fatalf("read: %q %v", data[:3], readErr)
	}
	if fresh.ETag == old.ETag || fresh.VersionID == old.VersionID || len(fresh.Metadata) != 0 {
		t.Fatalf("host edit retained old metadata: old=%+v fresh=%+v", old, fresh)
	}
	_, err = store.PutObject(ctx, multipartHostBucket, "artifact", bytes.NewReader([]byte("overwrite")), storage.PutOptions{IfMatch: old.ETag})
	if !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("stale overwrite: %v", err)
	}
}

func TestLegacyMultipartHostEditFailsClosed(t *testing.T) {
	store, root, old := multipartHostFixture(t, 2)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var buckets map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal(document["buckets"], &buckets); err != nil {
		t.Fatal(err)
	}
	delete(buckets[multipartHostBucket]["artifact"], "content_etag")
	document["buckets"], err = json.Marshal(buckets)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath(root), data, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "artifact")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("new"), 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	store = reopenMultipartStore(t, store, root)
	assertMultipartHostEdit(t, store, old)
}

func TestMultipartTimestampOnlyChangePreservesIdentity(t *testing.T) {
	store, root, old := multipartHostFixture(t, 2)
	path := filepath.Join(root, "artifact")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.HeadObject(context.Background(), multipartHostBucket, "artifact")
	if err != nil || fresh.ETag != old.ETag || fresh.VersionID != old.VersionID || fresh.Metadata["owner"] != "original" {
		t.Fatalf("timestamp-only change discarded identity: %+v %v", fresh, err)
	}
}

func multipartHostFixture(t *testing.T, count int) (*Store, string, *storage.ObjectMeta) {
	t.Helper()
	root := t.TempDir()
	store, err := New(Options{Root: root, Bucket: multipartHostBucket})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	upload, err := store.CreateMultipartUpload(ctx, multipartHostBucket, "artifact", storage.MultipartOptions{Metadata: map[string]string{"owner": "original"}})
	if err != nil {
		t.Fatal(err)
	}
	var parts []storage.PartInfo
	for number := 1; number <= count; number++ {
		data := []byte("old")
		if number < count {
			data = bytes.Repeat([]byte("x"), 5<<20)
		}
		part, err := store.UploadPart(ctx, upload.UploadID, number, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, *part)
	}
	object, err := store.CompleteMultipartUpload(ctx, upload.UploadID, parts)
	if err != nil {
		t.Fatal(err)
	}
	return store, root, object
}

func reopenMultipartStore(t *testing.T, previous *Store, root string) *Store {
	t.Helper()
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := New(Options{Root: root, Bucket: multipartHostBucket})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
