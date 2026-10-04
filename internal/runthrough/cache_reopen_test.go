package runthrough_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	filesystem "github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

func TestCacheReopenDoesNotRestartTTL(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cache, err := filesystem.NewFilesystemStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.PutObject(ctx, "bucket", "key", strings.NewReader("retained bytes"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	reopened, err := filesystem.NewFilesystemStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	up := newMockUpstream()
	adapter := runthrough.NewWithCache(runthrough.Config{Offline: true, Policy: runthrough.PolicyReadThroughCache, Cache: runthrough.CachePolicy{TTL: 10 * time.Millisecond}}, local, reopened, up)
	if err := adapter.ReconcileCacheIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if reader, _, err := adapter.GetObject(ctx, "bucket", "key"); !errors.Is(err, storage.ErrObjectNotFound) {
		if reader != nil {
			reader.Close()
		}
		t.Fatalf("reopen served expired bytes: %v", err)
	}
	if up.getCalls != 0 || up.headCalls != 0 {
		t.Fatal("offline reopen contacted provider")
	}
}
