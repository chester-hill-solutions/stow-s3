package runtime

import (
	"context"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestAbortKeepsExistingMultipartTargetUnreserved(t *testing.T) {
	ctx := context.Background()
	instance, err := Open(Options{MaxObjects: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "assets", "existing", []byte("x"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	first, err := instance.CreateMultipartUpload(ctx, "assets", "existing", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.CreateMultipartUpload(ctx, "assets", "existing", storage.MultipartOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := instance.AbortMultipartUpload(ctx, first.UploadID); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "assets", "second", []byte("y"), PutOptions{}); err != nil {
		t.Fatalf("abort invented a reservation for an existing object: %v", err)
	}
}
