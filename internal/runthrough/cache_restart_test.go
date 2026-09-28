package runthrough_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The cache lives in a directory and the eviction index lives in memory, so the
// two do not survive a restart together. A restarted server therefore comes up
// holding cache objects on disk and an index that has never heard of them.
//
// That is not an accounting curiosity. collectCacheCandidates plans eviction
// from the index and from nothing else, so an index that does not know about the
// objects already cached computes a cache total from the objects written since
// startup. A byte limit the operator set is then enforced against that total,
// and the cache grows past the bound with nothing to indicate it — which is the
// failure mode ReconcileCacheIndex's own comment says the index is not allowed
// to have.
//
// So this is a restart, modelled the only way it can be modelled in process: one
// cache store shared by two adapters, the second constructed over a store the
// first already filled.
func TestARestartedServerRebuildsTheEvictionIndexFromTheCacheOnDisk(t *testing.T) {
	ctx := context.Background()
	const objects = 40
	bodySize := 64

	// The cache store stands in for the cache directory: it outlives the adapter,
	// which is the whole property under test.
	cache := storage.NewMemoryStore()
	if err := cache.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create cache bucket: %v", err)
	}
	upstream := newMockUpstream()
	putProbeObjects(t, upstream, objects, bodySize)

	newAdapter := func() *runthrough.Adapter {
		local := storage.NewMemoryStore()
		if err := local.CreateBucket(ctx, "bucket"); err != nil {
			t.Fatalf("create local bucket: %v", err)
		}
		return runthrough.NewWithCache(runthrough.Config{
			Policy:     runthrough.PolicyReadThroughCache,
			Revalidate: true,
			Cache:      runthrough.CachePolicy{MaxBytes: 1 << 30},
		}, local, cache, upstream)
	}

	// The first process fills the cache and learns it in memory.
	first := newAdapter()
	warm(t, first, 0, objects)
	if got := first.CacheIndexEntries(); got != objects {
		t.Fatalf("first process index holds %d entries, want %d", got, objects)
	}

	// The second process is the restart: same cache store, new in-memory index.
	second := newAdapter()
	if got := second.CacheIndexEntries(); got != 0 {
		t.Fatalf("index holds %d entries before the restart reconciles it, want 0 — "+
			"if this is nonzero the test is not modelling a restart", got)
	}
	if err := second.ReconcileCacheIndex(ctx); err != nil {
		t.Fatalf("ReconcileCacheIndex: %v", err)
	}
	if got := second.CacheIndexEntries(); got != objects {
		t.Errorf("after reconciliation the index holds %d entries, want the %d objects on disk: "+
			"eviction plans from the index, so an index that does not know the cache enforces the wrong limit",
			got, objects)
	}
}

// The reconciliation is only worth anything if the limit it feeds is then
// enforced against the objects the restarted process never wrote. This asserts
// the consequence rather than the bookkeeping: a cache already over its bound
// must be brought back under it by the restarted server, using only what it
// found on disk.
func TestARestartedServerEnforcesTheByteLimitAgainstWhatItFoundOnDisk(t *testing.T) {
	ctx := context.Background()
	const objects = 40
	bodySize := 64
	total := int64(objects * bodySize)

	cache := storage.NewMemoryStore()
	if err := cache.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create cache bucket: %v", err)
	}
	upstream := newMockUpstream()
	putProbeObjects(t, upstream, objects, bodySize)

	local := func() storage.Store {
		s := storage.NewMemoryStore()
		if err := s.CreateBucket(ctx, "bucket"); err != nil {
			t.Fatalf("create local bucket: %v", err)
		}
		return s
	}

	// Fill the cache under a generous limit, as a previous run would have.
	first := runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache, Revalidate: true,
		Cache: runthrough.CachePolicy{MaxBytes: 1 << 30},
	}, local(), cache, upstream)
	warm(t, first, 0, objects)

	// The restart comes up under a limit the on-disk cache already exceeds.
	limit := total / 2
	second := runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache, Revalidate: true,
		Cache: runthrough.CachePolicy{MaxBytes: limit},
	}, local(), cache, upstream)
	if err := second.ReconcileCacheIndex(ctx); err != nil {
		t.Fatalf("ReconcileCacheIndex: %v", err)
	}

	// One more read after the restart is a cache write, and every cache write
	// re-plans eviction against the whole cache.
	rc, _, err := second.GetObject(ctx, "bucket", cacheProbeKey(0))
	if err != nil {
		t.Fatalf("post-restart GetObject: %v", err)
	}
	_ = rc.Close()

	var used int64
	token := ""
	for {
		page, err := cache.ListObjectsV2(ctx, "bucket", storage.ListOptions{
			MaxKeys: 1000, ContinuationToken: token,
		})
		if err != nil {
			t.Fatalf("list cache: %v", err)
		}
		for _, o := range page.Objects {
			used += o.Size
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			break
		}
		token = page.NextContinuationToken
	}
	if used > limit {
		t.Errorf("cache holds %d bytes against a %d byte limit after the restart evicted: "+
			"the bound is enforced against what this process wrote, not what it found", used, limit)
	}
	if used >= total {
		t.Errorf("cache holds %d bytes and evicted nothing, so the limit was never applied at all", used)
	}
}

var _ = bytes.Repeat
var _ = fmt.Sprintf
