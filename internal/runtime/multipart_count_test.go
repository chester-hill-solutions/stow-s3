package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestMultipartCountIncludesRepeatedTargetsAndReleases(t *testing.T) {
	ctx := context.Background()
	instance, err := Open(Options{MaxMultipartUploads: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	first := mustCountUpload(t, instance)
	second := mustCountUpload(t, instance)
	assertUploadCountFull(t, instance)
	if err := instance.AbortMultipartUpload(ctx, first.UploadID); err != nil {
		t.Fatal(err)
	}
	third := mustCountUpload(t, instance)
	assertUploadCountFull(t, instance)
	part, err := instance.UploadPart(ctx, second.UploadID, 1, bytes.NewBufferString("body"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.CompleteMultipartUpload(ctx, second.UploadID, []storage.PartInfo{*part}); err != nil {
		t.Fatal(err)
	}
	// Existing committed targets count too; each upload consumes its own slot.
	mustCountUpload(t, instance)
	assertUploadCountFull(t, instance)
	if err := instance.AbortMultipartUpload(ctx, third.UploadID); err != nil {
		t.Fatal(err)
	}
}

func mustCountUpload(t *testing.T, instance *Instance) *storage.MultipartUpload {
	t.Helper()
	upload, err := instance.CreateMultipartUpload(context.Background(), "bucket", "same-target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return upload
}

func assertUploadCountFull(t *testing.T, instance *Instance) {
	t.Helper()
	if _, err := instance.CreateMultipartUpload(context.Background(), "bucket", "same-target", storage.MultipartOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("full admission=%v", err)
	}
}

func TestMultipartCountReopenRefusesOverLimitWithoutDeletingUploads(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := store.CreateMultipartUpload(ctx, "bucket", "same-target", storage.MultipartOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := OpenWithStore(Options{Backend: BackendFilesystem, MaxMultipartUploads: 2}, store, nil); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("open over count limit=%v", err)
	}
	uploads, err := store.ListMultipartUploads(ctx, "bucket", storage.MultipartListOptions{MaxUploads: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(uploads.Uploads) != 3 {
		t.Fatal("refused open deleted existing uploads")
	}
	instance, err := OpenWithStore(Options{Backend: BackendFilesystem, MaxMultipartUploads: 3}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	assertUploadCountFull(t, instance)
	if err := instance.AbortMultipartUpload(ctx, uploads.Uploads[0].UploadID); err != nil {
		t.Fatal(err)
	}
	mustCountUpload(t, instance)
}

func TestMultipartCountDefaultAndValidation(t *testing.T) {
	instance, err := Open(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if instance.Capabilities().MaxMultipartUploads != DefaultMaxMultipartUploads {
		t.Fatal("incorrect default")
	}
	if _, err := Open(Options{MaxMultipartUploads: -1}); err == nil {
		t.Fatal("accepted negative count")
	}
}
