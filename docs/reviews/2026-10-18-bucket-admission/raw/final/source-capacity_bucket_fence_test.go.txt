package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestBucketCapacityFenceFailureRetainsMetadata(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires enforced non-root directory permissions")
	}
	ctx := context.Background()
	ns := bucketCapacityNamespace(t, 1<<20)
	corridor := filepath.Join(t.TempDir(), "corridor")
	dir := filepath.Join(corridor, "store")
	store := bucketCapacityStore(t, ns, dir)
	t.Cleanup(func() { _ = os.Chmod(corridor, 0755) })
	if err := os.Chmod(corridor, 0300); err != nil {
		t.Fatal(err)
	}
	err := store.CreateBucket(ctx, "uncertain")
	if !errors.Is(err, capacity.ErrUnknown) || !errors.Is(err, os.ErrPermission) || errors.Is(err, storage.ErrMutationCommitted) {
		t.Errorf("bucket fence result=%v", err)
	}
	info, err := store.HeadBucket(ctx, "uncertain")
	if err != nil {
		t.Fatalf("fence fault occurred before metadata publication: %v", err)
	}
	if err := os.Chmod(corridor, 0755); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := bucketCapacityStore(t, ns, dir)
	observed, err := reopened.HeadBucket(ctx, "uncertain")
	if err != nil || !observed.CreationDate.Equal(info.CreationDate) {
		t.Fatalf("recovery changed bucket metadata: %+v, %v", observed, err)
	}
	assertBucketCapacityMatchesFiles(t, reopened, ns)
	if err := reopened.DeleteBucket(ctx, "uncertain"); err != nil {
		t.Fatal(err)
	}
}
