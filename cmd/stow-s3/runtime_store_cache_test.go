package main

import (
	"context"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// runThroughRuntimeStore is the store the S3 server actually holds in run-through
// mode, and the admin route discovers the cache by a type assertion on it. Every
// provider the route asks for is forwarded here by hand, one method at a time.
//
// That is the shape of the bug this file exists for: the listing was implemented on
// the adapter and never forwarded, so the route answered with an empty list. An
// empty list is the most misleading possible answer to the question an offline agent
// asks — it reads as "the cache is empty" rather than as "nobody wired this up" —
// and nothing failed. The route was green, the adapter was green, and the feature
// was absent in production while present in both its own tests.
func TestTheRuntimeStoreForwardsTheCacheListing(t *testing.T) {
	ctx := context.Background()
	local, cache := storage.NewMemoryStore(), storage.NewMemoryStore()
	_ = local.CreateBucket(ctx, "bucket")
	_ = cache.CreateBucket(ctx, "bucket")

	adapter := runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache,
	}, local, cache, nil)
	store := &runThroughRuntimeStore{admin: adapter}

	// The assertion is the type assertion the admin route performs, written out. If
	// this stops holding, the route silently answers with an empty cache.
	provider, ok := interface{}(store).(interface {
		CachedObjects() []runthrough.CachedObject
	})
	if !ok {
		t.Fatal("the runtime store does not satisfy the cache listing interface, so /_stow/inspect reports an empty cache")
	}
	if listed := provider.CachedObjects(); listed == nil {
		t.Fatal("the forwarding returned nil rather than an empty listing, so a JSON encoder writes null instead of []")
	}
}

// Every provider the admin route asserts on is listed here, so a new one that is
// added to the route without a forwarding method fails to compile rather than
// answering with nothing. The interface is the set of things /_stow/inspect and
// /_stow/status can ask for; a method missing from it is a method whose absence
// nobody notices.
var _ = []interface{}{
	interface{ CacheStats() (uint64, uint64) }(&runThroughRuntimeStore{}),
	interface{ CacheEvictions() uint64 }(&runThroughRuntimeStore{}),
	interface {
		CachedObjects() []runthrough.CachedObject
	}(&runThroughRuntimeStore{}),
}
