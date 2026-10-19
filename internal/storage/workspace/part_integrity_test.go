package workspace_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestMultipartCompletionVerifiesRecordedPayload(t *testing.T) {
	store, root := newStore(t)
	ctx := context.Background()
	upload, err := store.CreateMultipartUpload(ctx, bucket, "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("payload")); err != nil {
		t.Fatal(err)
	}
	parts, err := store.ListParts(ctx, upload.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(workspace.InternalPath(root, "multipart", upload.UploadID), "part-1")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteMultipartUpload(ctx, upload.UploadID, parts); !errors.Is(err, storage.ErrInvalidPart) {
		t.Fatalf("corrupt payload completion=%v", err)
	}
	if _, _, err := store.GetObject(ctx, bucket, "target"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("corrupt completion published object: %v", err)
	}
	if _, err := store.GetMultipartUpload(ctx, upload.UploadID); err != nil {
		t.Fatalf("corrupt completion discarded staging: %v", err)
	}
}
