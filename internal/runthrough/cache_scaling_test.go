package runthrough_test

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// Does turning the cache limits on make writes quadratic?
//
// evictCache calls collectCacheCandidates, which lists every bucket and then
// every object in the cache, and trackCacheObject calls evictCache on each cache
// write. A cache holding N objects therefore costs a full scan per write, so
// filling it costs O(N^2) — but only when a limit is configured, because
// evictCache returns early when MaxBytes, MaxObjects and TTL are all zero.
//
// The limits are what change the cost, which is worth knowing before recommending
// cache settings for a host with a constrained link. This measures the shape
// rather than asserting it: the ratio between two equal-length batches at
// different cache depths distinguishes linear from quadratic without depending on
// the machine's speed.

const cacheProbeBody = 64

// seedCachingAdapter returns an adapter with n objects in upstream AND in the
// cache, so a measured read starts from a cache of known depth rather than
// filling one.
//
// The warm-up matters: an earlier version of this test seeded only upstream, so
// both measured batches filled an empty cache and did identical work. The ratio
// came out at 0.90x and looked like proof there was no scan at all, when in fact
// the two arms were the same experiment.
func seedCachingAdapter(t testing.TB, cacheMaxBytes int64, n int) (*runthrough.Adapter, *countingStore, *mockUpstream) {
	t.Helper()
	ctx := context.Background()
	local := storage.NewMemoryStore()
	cache := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create local bucket: %v", err)
	}
	if err := cache.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create cache bucket: %v", err)
	}
	counted := &countingStore{Store: cache}
	upstream := newMockUpstream()
	adapter := runthrough.NewWithCache(runthrough.Config{
		Policy:     runthrough.PolicyReadThroughCache,
		Revalidate: true,
		Cache:      runthrough.CachePolicy{MaxBytes: cacheMaxBytes},
	}, local, counted, upstream)

	// Populate upstream, then read every key once so the cache fills.
	putProbeObjects(t, upstream, n, cacheProbeBody)
	warm(t, adapter, 0, n)
	return adapter, counted, upstream
}

func putProbeObjects(t testing.TB, upstream *mockUpstream, n, size int) {
	t.Helper()
	ctx := context.Background()
	body := bytes.Repeat([]byte("x"), size)
	for i := 0; i < n; i++ {
		key := cacheProbeKey(i)
		if err := upstream.PutObject(ctx, "bucket", key, bytes.NewReader(body), storage.PutOptions{}); err != nil {
			t.Fatalf("seed upstream %s: %v", key, err)
		}
	}
}

func cacheProbeKey(i int) string { return fmt.Sprintf("object-%06d", i) }

// warm reads every key once, filling the cache. Untimed: it is setup, and it is
// itself quadratic, which is a separate observation.
func warm(t testing.TB, adapter *runthrough.Adapter, from, to int) {
	t.Helper()
	for i := from; i < to; i++ {
		rc, _, err := adapter.GetObject(context.Background(), "bucket", cacheProbeKey(i))
		if err != nil {
			t.Fatalf("warm GetObject(%d): %v", i, err)
		}
		_ = rc.Close()
	}
}

// changedEverywhere rewrites every upstream object with a different size, so each
// cached copy is stale. That is what makes a read take refreshFromUpstream, and
// refreshFromUpstream is the only caller of trackCacheObject, which is the only
// caller of evictCache. Without this the scan never runs.
func changedEverywhere(t testing.TB, upstream *mockUpstream, n int) {
	t.Helper()
	putProbeObjects(t, upstream, n, cacheProbeBody+1)
}

// countingStore wraps a store and counts the rows the eviction scan examines.
//
// Two earlier instruments were tried and both were wrong. A wall-clock threshold
// passed with the early return removed, because a 200-object scan is tens of
// milliseconds and the threshold was seconds. Counting listing *calls* was also
// flat — 80 for 40 refreshes at both 40 and 160 objects — because listAllObjects
// pages at MaxKeys 1000, so both depths fit one page and the call count cannot
// see the work. What actually costs is the rows walked, so that is what is
// counted.
type countingStore struct {
	storage.Store
	rowsExamined atomic.Int64
}

func (c *countingStore) ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	result, err := c.Store.ListObjectsV2(ctx, bucket, opts)
	if err == nil {
		c.rowsExamined.Add(int64(len(result.Objects)))
	}
	return result, err
}

// With no cache limit configured, a changed-object refresh must not list the
// cache at all. evictCache returns early when MaxBytes, MaxObjects and TTL are
// all zero, and this is the assertion that would notice if that early return were
// ever removed.
func TestCacheRefreshesWithoutALimitDoNotListTheCache(t *testing.T) {
	adapter, cache, upstream := seedCachingAdapter(t, 0, 200)
	changedEverywhere(t, upstream, 200)
	before := cache.rowsExamined.Load()
	warm(t, adapter, 0, 200)
	examined := cache.rowsExamined.Load() - before

	if examined != 0 {
		t.Errorf("200 changed refreshes with no cache limit walked %d cache rows; the early return in evictCache is gone", examined)
	}
	t.Logf("200 changed refreshes, no cache limit: %d cache rows examined", examined)
}

// With a limit configured, a changed-object refresh must not walk the cache at
// all. Eviction is planned from the adapter's own index, so the cost of a refresh
// no longer scales with how full the cache is.
//
// This test used to assert the opposite. It counted refreshes x depth — 1600 rows
// for 40 refreshes at depth 40, 6400 at depth 160 — and failed when the count did
// not grow with depth. The fix made the count zero, so the test failed, which is
// the intended direction: the property worth pinning is that planning reads
// nothing from the store, not that it reads a predictable amount.
func TestLimitedCacheRefreshesDoNotWalkTheCache(t *testing.T) {
	const batch = 40
	const limit = 1 << 30

	measure := func(depth int) int64 {
		adapter, cache, upstream := seedCachingAdapter(t, limit, depth)
		changedEverywhere(t, upstream, depth)
		before := cache.rowsExamined.Load()
		for i := 0; i < batch; i++ {
			rc, _, err := adapter.GetObject(context.Background(), "bucket", cacheProbeKey(i))
			if err != nil {
				t.Fatalf("GetObject(%d): %v", i, err)
			}
			_ = rc.Close()
		}
		return cache.rowsExamined.Load() - before
	}

	shallow := measure(batch)
	deep := measure(batch * 4)

	t.Logf("%d refreshes against a %d-object cache: %d cache rows examined; against %d: %d rows",
		batch, batch, shallow, batch*4, deep)

	if shallow != 0 || deep != 0 {
		t.Errorf("a refresh still walks the cache: %d rows at depth %d, %d at depth %d; "+
			"eviction should be planned from the index", shallow, batch, deep, batch*4)
	}
}

// The index is maintained on every path that populates or deletes from the cache,
// which is what makes planning free. If that stops being true the index drifts, the
// byte limit is enforced against the wrong number, and the cache grows past the
// bound the operator set — silently, because nothing about a drifted index is
// observable from the outside.
//
// ReconcileCacheIndex exists to detect exactly that, so it is asserted against a
// real workload rather than assumed.
func TestTheEvictionIndexAgreesWithTheCache(t *testing.T) {
	adapter, _, upstream := seedCachingAdapter(t, 1<<30, 60)
	changedEverywhere(t, upstream, 60)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		rc, _, err := adapter.GetObject(ctx, "bucket", cacheProbeKey(i))
		if err != nil {
			t.Fatalf("refresh %d: %v", i, err)
		}
		_ = rc.Close()
	}

	if err := adapter.ReconcileCacheIndex(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := adapter.CacheIndexEntries(); got != 60 {
		t.Fatalf("index holds %d entries after reconciliation, want the 60 objects in the cache", got)
	}
}

// A delete outside the tracked paths must not leave the index claiming an object
// the cache no longer has. This is the drift case, injected directly: the object
// is removed from the store without going through the adapter, which is what an
// out-of-band deletion or a future untracked path would look like.
func TestReconciliationDropsIndexEntriesForObjectsThatAreGone(t *testing.T) {
	adapter, cache, _ := seedCachingAdapter(t, 1<<30, 30)
	ctx := context.Background()

	if err := cache.DeleteObject(ctx, "bucket", cacheProbeKey(0)); err != nil {
		t.Fatalf("out-of-band delete: %v", err)
	}
	if got := adapter.CacheIndexEntries(); got != 30 {
		t.Fatalf("index holds %d entries before reconciliation, want 30", got)
	}

	if err := adapter.ReconcileCacheIndex(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := adapter.CacheIndexEntries(); got != 29 {
		t.Fatalf("index holds %d entries after reconciliation, want 29", got)
	}
}
