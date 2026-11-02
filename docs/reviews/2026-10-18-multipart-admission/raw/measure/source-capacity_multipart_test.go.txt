package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestMultipartPartsRespectNamespaceBudget(t *testing.T) {
	ctx := context.Background()
	namespace := publicationNamespace(t)
	store := publicationStore(t, namespace, t.TempDir())
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "large", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	partPath := filepath.Join(store.multipartDir(upload.UploadID), "part-00001")
	_, err = store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader(strings.Repeat("x", 5<<20)))
	if !errors.Is(err, capacity.ErrFull) {
		t.Fatalf("oversized multipart part error = %v, want capacity.ErrFull", err)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("over-budget multipart part was published: %v", err)
	}
}

func TestMultipartAbortReleasesNamespaceCapacity(t *testing.T) {
	ctx := context.Background()
	namespace := publicationNamespace(t)
	store := publicationStore(t, namespace, t.TempDir())
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "small", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("part")); err != nil {
		t.Fatal(err)
	}
	before, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.PayloadBytes == 0 || before.RecoveryBytes == 0 {
		t.Fatalf("multipart resources were not charged: %+v", before)
	}
	if err := store.AbortMultipartUpload(ctx, upload.UploadID); err != nil {
		t.Fatal(err)
	}
	after, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.PayloadBytes != 0 || after.RecoveryBytes >= before.RecoveryBytes {
		t.Fatalf("abort did not release multipart capacity: before=%+v after=%+v", before, after)
	}
}

func TestMultipartCompletionReleasesPartCapacity(t *testing.T) {
	ctx := context.Background()
	namespace := publicationNamespace(t)
	store := publicationStore(t, namespace, t.TempDir())
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "complete", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("part"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
		t.Fatal(err)
	}
	objectInfo, err := os.Stat(store.objectPath("bucket", "complete"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PayloadBytes != objectInfo.Size() {
		t.Fatalf("completion payload charge = %d, want only the committed object size %d", snapshot.PayloadBytes, objectInfo.Size())
	}
}

func TestMultipartManifestRecoveryBudget(t *testing.T) {
	ctx := context.Background()
	ns := publicationNamespace(t)
	store := publicationStore(t, ns, t.TempDir())
	_, err := store.CreateMultipartUpload(ctx, "bucket", "large", storage.MultipartOptions{Metadata: map[string]string{"large": strings.Repeat("x", 1<<20)}})
	if !errors.Is(err, capacity.ErrFull) {
		t.Errorf("over-budget manifest accepted: %v", err)
	}
	uploads, err := store.ListMultipartUploads(ctx, "bucket", storage.MultipartListOptions{})
	if err != nil || len(uploads.Uploads) != 0 {
		t.Fatalf("refused manifest remained visible: err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(store.dataDir, ".multipart"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("refused manifest left directories: %d, %v", len(entries), err)
	}
}

func TestMultipartRejectedReplacementPreservesPart(t *testing.T) {
	ctx := context.Background()
	ns := publicationNamespace(t)
	store := publicationStore(t, ns, t.TempDir())
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("original"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader(strings.Repeat("x", 5<<20)))
	if !errors.Is(err, capacity.ErrFull) {
		t.Errorf("over-budget replacement accepted: %v", err)
	}
	parts, err := store.ListParts(ctx, upload.UploadID)
	if err != nil || len(parts) != 1 || parts[0].ETag != first.ETag || parts[0].Size != first.Size {
		t.Fatalf("refused replacement changed part: %+v, %v", parts, err)
	}
	snapshot, err := ns.Snapshot(ctx)
	if err != nil || snapshot.PayloadBytes != first.Size || snapshot.PendingBytes != 0 {
		t.Fatalf("replacement changed charge: %+v, %v", snapshot, err)
	}
}

func TestMultipartManifestChargeMatchesScan(t *testing.T) {
	ctx := context.Background()
	ns := publicationNamespace(t)
	store := publicationStore(t, ns, t.TempDir())
	if _, err := store.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"label": "kept"}}); err != nil {
		t.Fatal(err)
	}
	before, err := ns.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.reconcileCapacityLocked(); err != nil {
		t.Fatal(err)
	}
	after, err := ns.Snapshot(ctx)
	if err != nil || after != before {
		t.Fatalf("manifest charge was stale: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestMultipartReplacementChargesOnePart(t *testing.T) {
	ctx := context.Background()
	ns := publicationNamespace(t)
	dir := t.TempDir()
	store := publicationStore(t, ns, dir)
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{1000000, 500000, 800000} {
		if _, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader(strings.Repeat("x", size))); err != nil {
			t.Fatal(err)
		}
		snapshot, err := ns.Snapshot(ctx)
		if err != nil || snapshot.PayloadBytes != int64(size) || snapshot.PendingBytes != 0 {
			t.Fatalf("replacement charge=%+v size=%d err=%v", snapshot, size, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFilesystemStoreWithNamespace(dir, ns)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.AbortMultipartUpload(ctx, upload.UploadID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ns.Snapshot(ctx)
	if err != nil || snapshot.PayloadBytes != 0 || snapshot.PendingBytes != 0 {
		t.Fatalf("reopened abort charge=%+v, %v", snapshot, err)
	}
}
