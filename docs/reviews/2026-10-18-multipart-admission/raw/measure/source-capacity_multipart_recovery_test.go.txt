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

func TestMultipartCompletionAdmissionRefusalRetainsUpload(t *testing.T) {
	ctx := context.Background()
	ns := publicationNamespace(t)
	store := publicationStore(t, ns, t.TempDir())
	id := multipartForCapacity(t, store)
	part, err := store.UploadPart(ctx, id, 1, strings.NewReader(strings.Repeat("x", 1100000)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteMultipartUpload(ctx, id, []storage.PartInfo{*part}); !errors.Is(err, capacity.ErrFull) {
		t.Fatalf("completion staging budget ignored: %v", err)
	}
	if _, err := store.HeadObject(ctx, "bucket", "key"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("refused completion published object: %v", err)
	}
	parts, err := store.ListParts(ctx, id)
	if err != nil || len(parts) != 1 || parts[0].ETag != part.ETag {
		t.Fatalf("refused completion lost parts: %+v, %v", parts, err)
	}
	if _, err := store.GetMultipartUpload(ctx, id); err != nil {
		t.Fatalf("refused completion lost manifest: %v", err)
	}
}

func TestMultipartBoundCompletionCleanupPreservesCommittedOutcome(t *testing.T) {
	ctx := context.Background()
	ns := publicationNamespace(t)
	store := publicationStore(t, ns, t.TempDir())
	id := multipartForCapacity(t, store)
	part, err := store.UploadPart(ctx, id, 1, strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	dir := store.multipartDir(id)
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755)
	meta, err := store.CompleteMultipartUpload(ctx, id, []storage.PartInfo{*part})
	if err == nil || meta == nil || !errors.Is(err, storage.ErrMutationCommitted) {
		t.Fatalf("cleanup result lost committed object: %+v, %v", meta, err)
	}
	if _, err := store.GetMultipartUpload(ctx, id); err != nil {
		t.Fatalf("cleanup lost upload evidence: %v", err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := store.AbortMultipartUpload(ctx, id); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.objectPath("bucket", "key"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ns.Snapshot(ctx)
	if err != nil || snapshot.PayloadBytes != info.Size() || snapshot.PendingBytes != 0 {
		t.Fatalf("cleanup recovery charge=%+v object=%d err=%v", snapshot, info.Size(), err)
	}
}

func TestMultipartCapacityHoldProtectsPart(t *testing.T) {
	ctx := context.Background()
	ns := publicationNamespace(t)
	store := publicationStore(t, ns, t.TempDir())
	id := multipartForCapacity(t, store)
	if _, err := store.UploadPart(ctx, id, 1, strings.NewReader("held")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.multipartDir(id), "part-00001")
	ref := resourceFor(path, []byte("held"), capacity.Payload)
	ref.StoreID = store.saves.id
	if err := ns.BeginOperation(ctx, capacity.Operation{ID: "hold", Owner: "owner", Meaning: "read-part", Dependencies: []capacity.Resource{ref}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UploadPart(ctx, id, 1, strings.NewReader("replacement")); !errors.Is(err, capacity.ErrHeld) {
		t.Fatalf("held replacement=%v", err)
	}
	if err := store.AbortMultipartUpload(ctx, id); !errors.Is(err, capacity.ErrHeld) {
		t.Fatalf("held abort=%v", err)
	}
	if err := ns.ReleaseOperation(ctx, "hold", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.AbortMultipartUpload(ctx, id); err != nil {
		t.Fatal(err)
	}
}
