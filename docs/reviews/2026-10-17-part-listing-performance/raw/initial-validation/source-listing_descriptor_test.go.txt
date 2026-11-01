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

func TestPartListingRetainsIndependentDescriptors(t *testing.T) {
	store, root := newStore(t)
	ctx := context.Background()
	upload, err := store.CreateMultipartUpload(ctx, bucket, "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[int]storage.PartInfo)
	for i, number := range []int{10000, 10, 2, 1, 100, 9} {
		part, err := store.UploadPart(ctx, upload.UploadID, number, strings.NewReader(strings.Repeat("x", i+1)))
		if err != nil {
			t.Fatal(err)
		}
		want[number] = *part
	}
	first := assertListedPartDescriptors(t, store, upload.UploadID, want)
	part, err := store.UploadPart(ctx, upload.UploadID, 2, strings.NewReader("replacement"))
	if err != nil {
		t.Fatal(err)
	}
	if first[1] != want[2] {
		t.Fatal("earlier listing changed before replacement was read")
	}
	want[2] = *part
	assertListedPartDescriptors(t, store, upload.UploadID, want)
	if first[1].ETag == part.ETag {
		t.Fatal("later listing changed a previously returned descriptor")
	}
	page, err := store.ListPartsPage(ctx, upload.UploadID, storage.ListPartsOptions{PartNumberMarker: 2, MaxParts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Parts) != 2 || page.Parts[0] != want[9] || page.Parts[1] != want[10] || !page.IsTruncated || page.NextPartNumberMarker != 10 {
		t.Fatalf("page=%+v", page)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workspace.New(workspace.Options{Root: root, Bucket: bucket})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertListedPartDescriptors(t, reopened, upload.UploadID, want)
	assertDamagedListingCanRecover(t, reopened, root, upload.UploadID, want)
}

func assertListedPartDescriptors(t *testing.T, store *workspace.Store, id string, want map[int]storage.PartInfo) []storage.PartInfo {
	t.Helper()
	parts, err := store.ListParts(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != len(want) {
		t.Fatalf("listing count=%d want=%d", len(parts), len(want))
	}
	previous := 0
	for _, part := range parts {
		if part.PartNumber <= previous || part != want[part.PartNumber] {
			t.Fatalf("descriptor=%+v want=%+v previous=%d", part, want[part.PartNumber], previous)
		}
		previous = part.PartNumber
	}
	return parts
}

func assertDamagedListingCanRecover(t *testing.T, store *workspace.Store, root, id string, want map[int]storage.PartInfo) {
	t.Helper()
	file := filepath.Join(workspace.InternalPath(root, "multipart", id), "part-10000")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, raw[:1], 0600); err != nil {
		t.Fatal(err)
	}
	parts, err := store.ListParts(context.Background(), id)
	if err == nil || parts != nil {
		t.Fatalf("damaged listing returned parts=%+v error=%v", parts, err)
	}
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	assertListedPartDescriptors(t, store, id, want)
}
