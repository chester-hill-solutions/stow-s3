package runthrough_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// CachedObjects exists so an agent that has lost the network can ask what it can
// still read. The three counters the inspection already reported — hits, misses,
// evictions — cannot answer that: a cache can post a perfect hit rate and hold
// nothing the agent asked for.
//
// These tests drive the real adapter rather than the index directly, because the
// listing's value depends on the index agreeing with the store. A listing built
// from the index that has drifted from the cache would name keys that are gone, and
// an agent would only find out by failing.

// cacheListingHarness is a run-through adapter with a separate cache, which is the
// only configuration where there is a cache to list.
type cacheListingHarness struct {
	adapter *runthrough.Adapter
	local   storage.Store
	cache   storage.Store
	up      *mockUpstream
	ctx     context.Context
}

func newCacheListingHarness(t *testing.T, ttl time.Duration) *cacheListingHarness {
	t.Helper()
	ctx := context.Background()
	local, cache := storage.NewMemoryStore(), storage.NewMemoryStore()
	_ = local.CreateBucket(ctx, "bucket")
	_ = cache.CreateBucket(ctx, "bucket")
	up := newMockUpstream()
	adapter := runthrough.NewWithCache(runthrough.Config{
		Policy:     runthrough.PolicyReadThroughCache,
		Revalidate: false,
		Cache:      runthrough.CachePolicy{TTL: ttl},
	}, local, cache, up)
	return &cacheListingHarness{adapter: adapter, local: local, cache: cache, up: up, ctx: ctx}
}

func (h *cacheListingHarness) warm(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		_, _ = h.up.PutObject(h.ctx, "bucket", key, bytes.NewReader([]byte("payload for "+key)), storage.PutOptions{})
		if _, _, err := h.adapter.GetObject(h.ctx, "bucket", key); err != nil {
			t.Fatalf("warm %q: %v", key, err)
		}
	}
}

func TestCachedObjectsNamesWhatIsActuallyInTheCache(t *testing.T) {
	h := newCacheListingHarness(t, 0)
	h.warm(t, "models/a.bin", "models/b.bin")

	listed := h.adapter.CachedObjects()
	if len(listed) != 2 {
		t.Fatalf("the listing holds %d entries, want 2: %+v", len(listed), listed)
	}
	byKey := map[string]runthrough.CachedObject{}
	for _, object := range listed {
		byKey[object.Key] = object
	}
	for _, key := range []string{"models/a.bin", "models/b.bin"} {
		object, present := byKey[key]
		if !present {
			t.Fatalf("the listing does not name %q: %+v", key, listed)
		}
		if object.Bucket != "bucket" {
			t.Errorf("%q is listed under bucket %q", key, object.Bucket)
		}
		if object.Size != int64(len("payload for "+key)) {
			t.Errorf("%q is listed at %d bytes, want %d", key, object.Size, len("payload for "+key))
		}
		if !object.Readable {
			t.Errorf("%q is listed as unreadable with no TTL set", key)
		}
	}
}

func TestCachedObjectsIsEmptyForAStoreWithNoSeparateCache(t *testing.T) {
	// The single-store mode has no cache to list, and must report that rather than
	// listing the local store's objects as if they were cached.
	ctx := context.Background()
	store := storage.NewMemoryStore()
	_ = store.CreateBucket(ctx, "bucket")
	_, _ = store.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("body")), storage.PutOptions{})
	adapter := runthrough.NewWithCache(runthrough.Config{Policy: runthrough.PolicyReadThroughCache}, store, store, newMockUpstream())
	if listed := adapter.CachedObjects(); len(listed) != 0 {
		t.Fatalf("a single-store adapter listed %+v as cached", listed)
	}
}

func TestCachedObjectsForgetsWhatTheCacheEvicted(t *testing.T) {
	// The listing is the index, and the index decides what gets evicted. If the two
	// disagree, the listing names a key the cache has already dropped and an agent
	// offline is told it can read something it cannot.
	ctx := context.Background()
	local, cache := storage.NewMemoryStore(), storage.NewMemoryStore()
	_ = local.CreateBucket(ctx, "bucket")
	_ = cache.CreateBucket(ctx, "bucket")
	up := newMockUpstream()
	_, _ = up.PutObject(ctx, "bucket", "keep", bytes.NewReader([]byte("aaaa")), storage.PutOptions{})
	_, _ = up.PutObject(ctx, "bucket", "drop", bytes.NewReader([]byte("bbbb")), storage.PutOptions{})

	adapter := runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache,
		Cache:  runthrough.CachePolicy{MaxObjects: 1},
	}, local, cache, up)

	if _, _, err := adapter.GetObject(ctx, "bucket", "keep"); err != nil {
		t.Fatalf("warm keep: %v", err)
	}
	if _, _, err := adapter.GetObject(ctx, "bucket", "drop"); err != nil {
		t.Fatalf("warm drop: %v", err)
	}
	if listed := adapter.CachedObjects(); len(listed) != 1 {
		t.Fatalf("after eviction to a limit of 1, the listing holds %d entries: %+v", len(listed), listed)
	}
	// And the one it names is the one the store still has.
	listed := adapter.CachedObjects()
	present, _ := cache.HeadObject(ctx, "bucket", listed[0].Key)
	if present == nil {
		t.Fatalf("the listing names %q, which the cache store does not hold", listed[0].Key)
	}
}

func TestCachedObjectsMarksAnExpiredEntryUnreadable(t *testing.T) {
	// `readable` is the reason the listing exists. An entry past its TTL stays in
	// the index until something reads it or a sweep runs, and telling an agent it
	// is readable would send it to a key that is about to be evicted under it.
	//
	// The TTL has to outlast the warming read: an entry whose TTL has already
	// passed is evicted by the read that cached it, so the index is empty by the
	// time anything could list it. The window this test needs is the one between
	// an entry expiring and anything touching it.
	h := newCacheListingHarness(t, 20*time.Millisecond)
	h.warm(t, "short-lived")
	if listed := h.adapter.CachedObjects(); len(listed) != 1 || !listed[0].Readable {
		t.Fatalf("a fresh entry is not listed as readable: %+v", listed)
	}

	time.Sleep(40 * time.Millisecond)

	listed := h.adapter.CachedObjects()
	if len(listed) != 1 {
		t.Fatalf("the listing holds %d entries, want the expired one still indexed: %+v", len(listed), listed)
	}
	if listed[0].Readable {
		t.Fatalf("an entry past its TTL is listed as readable: %+v", listed[0])
	}
	if listed[0].ExpiresAt.IsZero() {
		t.Errorf("an entry with a TTL is listed without an expiry: %+v", listed[0])
	}
}

func TestCachedObjectsOrdersOldestAccessFirst(t *testing.T) {
	// An agent about to be cut off wants the keys least likely to still be there,
	// and an operator wants the eviction order. Both are the same order.
	h := newCacheListingHarness(t, 0)
	h.warm(t, "first", "second", "third")

	listed := h.adapter.CachedObjects()
	if len(listed) != 3 {
		t.Fatalf("the listing holds %d entries, want 3", len(listed))
	}
	for i := 1; i < len(listed); i++ {
		if listed[i].Accessed.Before(listed[i-1].Accessed) {
			t.Fatalf("entry %d was accessed before entry %d, so the listing is not oldest-first: %+v", i, i-1, listed)
		}
	}
}

func TestCachedObjectsSurvivesARestart(t *testing.T) {
	// The cache directory outlives the process, so a restarted server has to be
	// able to answer the same question. This is the reconcile path: the index
	// starts empty and is rebuilt from the store.
	ctx := context.Background()
	local, cache := storage.NewMemoryStore(), storage.NewMemoryStore()
	_ = local.CreateBucket(ctx, "bucket")
	_ = cache.CreateBucket(ctx, "bucket")
	up := newMockUpstream()
	_, _ = up.PutObject(ctx, "bucket", "survivor", bytes.NewReader([]byte("payload")), storage.PutOptions{})

	first := runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache,
	}, local, cache, up)
	if _, _, err := first.GetObject(ctx, "bucket", "survivor"); err != nil {
		t.Fatalf("warm: %v", err)
	}

	// A second adapter over the same cache directory, as a restart would build.
	restarted := runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache,
	}, local, cache, up)
	if err := restarted.ReconcileCacheIndex(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	listed := restarted.CachedObjects()
	if len(listed) != 1 || listed[0].Key != "survivor" {
		t.Fatalf("after a restart the listing is %+v, want the one cached key", listed)
	}
}
