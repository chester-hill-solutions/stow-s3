package fs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func holdStore(t *testing.T, dir string) *FilesystemStore {
	t.Helper()
	store, err := NewFilesystemStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func putBytes(t *testing.T, store *FilesystemStore, bucket, key, body string) {
	t.Helper()
	if err := store.CreateBucket(context.Background(), bucket); err != nil && !errors.Is(err, storage.ErrBucketExists) {
		t.Fatal(err)
	}
	meta, err := store.PutObject(context.Background(), bucket, key, bytes.NewReader([]byte(body)), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != int64(len(body)) {
		t.Fatalf("put %s/%s = %+v", bucket, key, meta)
	}
}

// pin is the object a consumer has read, which is what a hold carries.
func pin(t *testing.T, store *FilesystemStore, bucket, key string) storage.ObjectResource {
	t.Helper()
	existing, data, err := store.readExistingLocked(bucket, key)
	if err != nil || existing == nil {
		t.Fatalf("observe %s/%s: %v", bucket, key, err)
	}
	return storage.ObjectResource{Bucket: bucket, Key: key, Guard: storage.WriteGuard{Fingerprint: storage.ObjectFingerprint(*existing, data)}}
}

func beginHold(t *testing.T, store *FilesystemStore, id, owner string, object storage.ObjectResource) error {
	t.Helper()
	return store.BeginRecoveryHold(context.Background(), storage.RecoveryHoldOptions{
		ID: id, Owner: owner, Objects: []storage.ObjectResource{object},
	})
}

func TestRecoveryHoldBlocksMutationUntilRelease(t *testing.T) {
	ctx := context.Background()
	store := holdStore(t, filepath.Join(t.TempDir(), "store"))
	putBytes(t, store, "reports", "result", "original")
	if err := beginHold(t, store, "delivery", "consumer", pin(t, store, "reports", "result")); err != nil {
		t.Fatal(err)
	}
	assertHeldCompletionAndCopyRefused(t, store)
	if _, err := store.PutObject(ctx, "reports", "result", bytes.NewReader([]byte("replacement")), storage.PutOptions{}); !errors.Is(err, storage.ErrRecoveryHeld) {
		t.Fatalf("held overwrite = %v", err)
	}
	if err := store.DeleteObject(ctx, "reports", "result"); !errors.Is(err, storage.ErrRecoveryHeld) {
		t.Fatalf("held deletion = %v", err)
	}
	// A batch is refused the same way, and says nothing about the rest: a caller
	// retrying the whole batch must not delete the keys around the held one.
	deleted, err := store.DeleteObjects(ctx, "reports", []string{"other", "result"})
	if !errors.Is(err, storage.ErrRecoveryHeld) || len(deleted) != 1 {
		t.Fatalf("held batch deletion = %v, %v", deleted, err)
	}
	if err := store.ReleaseRecoveryHold(ctx, "delivery", "consumer"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteObject(ctx, "reports", "result"); err != nil {
		t.Fatalf("released deletion: %v", err)
	}
}

// Every write of the object is refused, not only the unconditional ones.
func assertHeldCompletionAndCopyRefused(t *testing.T, store *FilesystemStore) {
	t.Helper()
	ctx := context.Background()
	upload, err := store.CreateMultipartUpload(ctx, "reports", "result", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part := bytes.Repeat([]byte("Z"), 5<<20)
	info, err := store.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(part))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*info}); !errors.Is(err, storage.ErrRecoveryHeld) {
		t.Fatalf("held completion = %v", err)
	}
	if err := store.AbortMultipartUpload(ctx, upload.UploadID); err != nil {
		t.Fatal(err)
	}
	putBytes(t, store, "archive", "elsewhere", "source")
	if _, err := store.CopyObject(ctx, "archive", "elsewhere", "reports", "result"); !errors.Is(err, storage.ErrRecoveryHeld) {
		t.Fatalf("held copy destination = %v", err)
	}
	object, err := store.readObject("reports", "result")
	if err != nil || string(object.Data) != "original" {
		t.Fatalf("object after refused writes = %q, %v", object.Data, err)
	}
}

func TestRecoveryHoldOutlivesTheStore(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "store")
	store := holdStore(t, dir)
	putBytes(t, store, "reports", "result", "original")
	if err := beginHold(t, store, "delivery", "consumer", pin(t, store, "reports", "result")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := holdStore(t, dir)
	if err := reopened.DeleteObject(ctx, "reports", "result"); !errors.Is(err, storage.ErrRecoveryHeld) {
		t.Fatalf("reopened held deletion = %v", err)
	}
	record, err := reopened.readObject("reports", "result")
	if err != nil || string(record.Data) != "original" {
		t.Fatalf("retained record = %q, %v", record.Data, err)
	}
	if err := reopened.ReleaseRecoveryHold(ctx, "delivery", "consumer"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.DeleteObject(ctx, "reports", "result"); err != nil {
		t.Fatalf("released deletion after reopen: %v", err)
	}
}

func TestRecoveryHoldIdentityIsEnforced(t *testing.T) {
	ctx := context.Background()
	store := holdStore(t, filepath.Join(t.TempDir(), "store"))
	putBytes(t, store, "reports", "result", "original")
	if err := beginHold(t, store, "delivery", "consumer", pin(t, store, "reports", "result")); err != nil {
		t.Fatal(err)
	}
	if err := beginHold(t, store, "delivery", "consumer", pin(t, store, "reports", "result")); err != nil {
		t.Fatalf("replayed begin: %v", err)
	}
	if err := store.ReleaseRecoveryHold(ctx, "delivery", "someone-else"); !errors.Is(err, capacity.ErrConflict) {
		t.Fatalf("foreign release = %v", err)
	}
	if err := store.ReleaseRecoveryHold(ctx, "delivery", "consumer"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseRecoveryHold(ctx, "delivery", "consumer"); err != nil {
		t.Fatalf("replayed release: %v", err)
	}
	if err := store.ReleaseRecoveryHold(ctx, "absent", "consumer"); !errors.Is(err, storage.ErrRecoveryHoldNotFound) {
		t.Fatalf("unknown hold = %v", err)
	}
	// Re-arming is the consumer saying it still needs the bytes: the hold comes
	// back rather than a conflict with its own past release.
	if err := beginHold(t, store, "delivery", "consumer", pin(t, store, "reports", "result")); err != nil {
		t.Fatalf("re-armed begin: %v", err)
	}
	if err := store.DeleteObject(ctx, "reports", "result"); !errors.Is(err, storage.ErrRecoveryHeld) {
		t.Fatalf("re-armed deletion = %v", err)
	}
}

func TestRecoveryHoldRefusesAStaleObservation(t *testing.T) {
	store := holdStore(t, filepath.Join(t.TempDir(), "store"))
	putBytes(t, store, "reports", "result", "original")
	guard := pin(t, store, "reports", "result")
	putBytes(t, store, "reports", "result", "replaced")
	err := store.BeginRecoveryHold(context.Background(), storage.RecoveryHoldOptions{
		ID: "delivery", Owner: "consumer", Objects: []storage.ObjectResource{guard},
	})
	if !errors.Is(err, storage.ErrInvalidRecoveryHold) {
		t.Fatalf("hold on bytes it did not observe = %v", err)
	}
}

// An unbounded hold list is a denial of service on the store's own writes.
func TestRecoveryHoldRefusesAMalformedRequest(t *testing.T) {
	ctx := context.Background()
	store := holdStore(t, filepath.Join(t.TempDir(), "store"))
	putBytes(t, store, "reports", "result", "original")
	object := pin(t, store, "reports", "result")
	guard := object.Guard
	for name, options := range map[string]storage.RecoveryHoldOptions{
		"no id":         {Owner: "consumer", Objects: []storage.ObjectResource{object}},
		"no owner":      {ID: "delivery", Objects: []storage.ObjectResource{object}},
		"padded id":     {ID: " delivery", Owner: "consumer", Objects: []storage.ObjectResource{object}},
		"no objects":    {ID: "delivery", Owner: "consumer"},
		"bad bucket":    {ID: "delivery", Owner: "consumer", Objects: []storage.ObjectResource{{Bucket: "..", Key: "result", Guard: guard}}},
		"unknown key":   {ID: "delivery", Owner: "consumer", Objects: []storage.ObjectResource{{Bucket: "reports", Key: "absent", Guard: guard}}},
		"duplicate":     {ID: "delivery", Owner: "consumer", Objects: []storage.ObjectResource{object, object}},
		"too many":      {ID: "delivery", Owner: "consumer", Objects: manyObjects(object)},
		"empty id":      {ID: "", Owner: "consumer", Objects: []storage.ObjectResource{object}},
		"control owner": {ID: "delivery", Owner: "consumer\n", Objects: []storage.ObjectResource{object}},
	} {
		if err := store.BeginRecoveryHold(ctx, options); !errors.Is(err, storage.ErrInvalidRecoveryHold) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := store.ReleaseRecoveryHold(ctx, "", "consumer"); !errors.Is(err, storage.ErrInvalidRecoveryHold) {
		t.Fatalf("empty release id: %v", err)
	}
}

func manyObjects(object storage.ObjectResource) []storage.ObjectResource {
	objects := make([]storage.ObjectResource, 0, maxRecoveryReferences+1)
	for index := 0; index <= maxRecoveryReferences; index++ {
		objects = append(objects, storage.ObjectResource{Bucket: "reports", Key: object.Key + string(rune('a'+index%26)) + string(rune('a'+index/26)), Guard: object.Guard})
	}
	return objects
}

// A journal nobody can read is not treated as no holds: deletes and overwrites
// proceeding on an unverified record is how the bytes a hold exists to keep are
// lost.
func TestRecoveryHoldRefusesUnreadableState(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "store")
	store := holdStore(t, dir)
	putBytes(t, store, "reports", "result", "original")
	if err := beginHold(t, store, "delivery", "consumer", pin(t, store, "reports", "result")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	journal := filepath.Join(dir, ".recovery-holds.json")
	original, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}

	for name, rewrite := range map[string]func(){
		"checksum mismatch": func() { corrupt(t, journal, []byte(`{"identity":"x","version":1,"holds":[],"checksum":"deadbeef"}`)) },
		"truncated":         func() { corrupt(t, journal, original[:len(original)/2]) },
		"unknown field":     func() { corrupt(t, journal, append(original, 'x')) },
		"unknown hold state": func() {
			corrupt(t, journal, bytes.Replace(original, []byte(`"state":"pending"`), []byte(`"state":"maybe"`), 1))
		},
		"not json":        func() { corrupt(t, journal, []byte("not json at all")) },
		"journal removed": func() { remove(t, journal) },
	} {
		corrupt(t, journal, original)
		rewrite()
		reopened := holdStore(t, dir)
		if err := reopened.DeleteObject(ctx, "reports", "result"); !errors.Is(err, storage.ErrInvalidRecoveryHold) {
			t.Fatalf("%s: deletion = %v", name, err)
		}
		if _, err := reopened.PutObject(ctx, "reports", "result", bytes.NewReader([]byte("replacement")), storage.PutOptions{}); !errors.Is(err, storage.ErrInvalidRecoveryHold) {
			t.Fatalf("%s: overwrite = %v", name, err)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// A store that never took a hold has neither file, and mutations are not
	// blocked by their absence.
	cleanDir := filepath.Join(t.TempDir(), "store")
	clean := holdStore(t, cleanDir)
	putBytes(t, clean, "reports", "result", "original")
	if err := clean.DeleteObject(ctx, "reports", "result"); err != nil {
		t.Fatalf("store that never took a hold: %v", err)
	}
	for _, path := range []string{filepath.Join(cleanDir, ".recovery-holds.json"), filepath.Join(cleanDir, ".recovery-holds.identity")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s written for a store with no holds", filepath.Base(path))
		}
	}
}

func corrupt(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func remove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
