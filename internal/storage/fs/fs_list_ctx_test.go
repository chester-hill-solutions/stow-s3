package fs_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

// A listing walks the whole bucket before it paginates, so a cancelled request
// must stop the walk rather than run to completion. The context was ignored
// before, which meant a large listing could not be abandoned.
func TestFilesystemStoreListHonorsCancellation(t *testing.T) {
	ctx := context.Background()
	store, err := fs.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := store.PutObject(ctx, "bucket", "key", strings.NewReader("body"), storage.PutOptions{}); err != nil {
		t.Fatalf("put object: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ListObjectsV2(cancelled, "bucket", storage.ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("list with a cancelled context = %v, want context.Canceled", err)
	}
}
