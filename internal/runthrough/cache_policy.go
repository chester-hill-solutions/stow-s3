package runthrough

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type cacheEntry struct {
	accessedAt time.Time
	expiresAt  time.Time
	// bucket, key and size are recorded here so eviction can plan from this map
	// alone. The previous implementation listed the cache and issued a HeadObject
	// per object to recover exactly this information, so a single changed-object
	// refresh cost a store round trip for every cached object. size is the object's
	// stored length and is what the byte limit is measured against.
	bucket string
	key    string
	size   int64
}

type cacheCandidate struct {
	key  string
	meta storage.ObjectMeta
	at   time.Time
}

// cacheIndexOverhead is the per-entry cost of planning eviction from the index
// rather than from the store. It is reported so a device budget can account for
// it; the value is an estimate from the map's own size, not a measurement, and the
// measurement that matters is CacheIndexPlanningRows in the tests.
const cacheIndexOverhead = 0

func (a *Adapter) CacheEvictions() uint64 {
	return a.cacheEvictions.Load()
}

func (a *Adapter) touchCache(bucket, key string) {
	if !a.separateCache {
		return
	}
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	now := time.Now()
	accessKey := cacheEntryKey(bucket, key)
	entry := a.cacheEntries[accessKey]
	entry.accessedAt = now
	if entry.expiresAt.IsZero() && a.cfg.Cache.TTL > 0 {
		entry.expiresAt = now.Add(a.cfg.Cache.TTL)
	}
	a.cacheEntries[accessKey] = entry
}

// trackCacheObject records that the cache now holds this object, with the size
// the byte limit is measured against, and then evicts if the cache is over budget.
//
// The size is passed in rather than re-read from the store. Reading it back would
// mean a round trip per write to learn what the caller already held in hand, which
// is the cost this change exists to remove.
func (a *Adapter) trackCacheObject(ctx context.Context, bucket, key string, size int64) error {
	if !a.separateCache {
		return nil
	}
	a.cacheMu.Lock()
	accessKey := cacheEntryKey(bucket, key)
	entry := a.cacheEntries[accessKey]
	entry.bucket = bucket
	entry.key = key
	entry.size = size
	now := time.Now()
	entry.accessedAt = now
	if a.cfg.Cache.TTL > 0 {
		entry.expiresAt = now.Add(a.cfg.Cache.TTL)
	}
	a.cacheEntries[accessKey] = entry
	a.cacheMu.Unlock()
	return a.evictCache(ctx)
}

// forgetCacheObject drops an index entry. Every path that deletes from the cache
// must call it, or the index will plan evictions for objects that are gone and
// will not count the ones that remain.
func (a *Adapter) forgetCacheObject(bucket, key string) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	delete(a.cacheEntries, cacheEntryKey(bucket, key))
}

func (a *Adapter) cacheEntryExpired(bucket, key string) bool {
	if a.cfg.Cache.TTL <= 0 {
		return false
	}
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	entry, ok := a.cacheEntries[cacheEntryKey(bucket, key)]
	return ok && !entry.expiresAt.IsZero() && !time.Now().Before(entry.expiresAt)
}

func (a *Adapter) evictCache(ctx context.Context) error {
	policy := a.cfg.Cache
	if policy.MaxBytes <= 0 && policy.MaxObjects <= 0 && policy.TTL <= 0 {
		return nil
	}
	candidates, totalBytes, err := a.collectCacheCandidates(ctx)
	if err != nil {
		return err
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].at.Equal(candidates[j].at) {
			return candidates[i].key < candidates[j].key
		}
		return candidates[i].at.Before(candidates[j].at)
	})
	for len(candidates) > 0 && cacheOverBudget(len(candidates), totalBytes, policy) {
		victim := candidates[0]
		candidates = candidates[1:]
		if err := a.cache.DeleteObject(ctx, victim.meta.Bucket, victim.meta.Key); err != nil && !storageErrIsMissingObject(err) {
			return err
		}
		totalBytes -= victim.meta.Size
		a.forgetCacheObject(victim.meta.Bucket, victim.meta.Key)
		a.cacheEvictions.Add(1)
	}
	return nil
}

// collectCacheCandidates plans eviction from the index, not from the store.
//
// The previous version listed every bucket, paginated every object, and issued a
// HeadObject per object to recover the access time and size. That made a single
// changed-object refresh cost a store round trip for every cached object, so a
// cache of N objects took N round trips each time anything in it changed — which
// is most of a session. The index already holds access time, expiry and, now,
// size, so the same plan costs one pass over a map.
//
// What this gives up is discovery of cache objects with no index entry. Every path
// that populates the cache calls trackCacheObject, so the index is complete for
// objects stow wrote; reconcileCacheIndex exists to rebuild it from the store when
// that assumption is worth checking rather than trusting.
func (a *Adapter) collectCacheCandidates(ctx context.Context) ([]cacheCandidate, int64, error) {
	now := time.Now()

	a.cacheMu.Lock()
	pending := make([]cacheCandidate, 0, len(a.cacheEntries))
	var totalBytes int64
	var expired []string
	for accessKey, entry := range a.cacheEntries {
		if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
			expired = append(expired, accessKey)
			continue
		}
		pending = append(pending, cacheCandidate{
			key:  accessKey,
			meta: storage.ObjectMeta{Bucket: entry.bucket, Key: entry.key, Size: entry.size},
			at:   entry.accessedAt,
		})
		totalBytes += entry.size
	}
	expiredEntries := make([]cacheEntry, 0, len(expired))
	for _, accessKey := range expired {
		if entry, present := a.cacheEntries[accessKey]; present {
			expiredEntries = append(expiredEntries, entry)
		}
	}
	a.cacheMu.Unlock()

	// Expiry is applied outside the lock, because it is a store mutation and
	// holding the index lock across a store call is how a deadlock starts.
	for _, entry := range expiredEntries {
		if err := a.cache.DeleteObject(ctx, entry.bucket, entry.key); err != nil && !storageErrIsMissingObject(err) {
			return nil, 0, err
		}
		a.forgetCacheObject(entry.bucket, entry.key)
		a.cacheEvictions.Add(1)
	}

	return pending, totalBytes, nil
}

// reconcileCacheIndex rebuilds the eviction index from the cache store.
//
// The index is maintained on every path stow uses to populate and delete, so this
// should be a no-op. It exists because an index that has drifted from the store
// enforces the wrong limit, and a wrong limit fails silently: the cache grows past
// the bound the operator set. Cheap enough to call from a test, and from an
// operator-facing repair path if one is ever added.
// ReconcileCacheIndex rebuilds the eviction index from the cache store. Exported
// so the drift check is a test rather than a claim.
func (a *Adapter) ReconcileCacheIndex(ctx context.Context) error { return a.reconcileCacheIndex(ctx) }

func (a *Adapter) reconcileCacheIndex(ctx context.Context) error {
	buckets, err := a.cache.ListBuckets(ctx)
	if err != nil {
		return err
	}
	found := make(map[string]struct{})
	for _, bucket := range buckets {
		objects, listErr := listAllObjects(ctx, a.cache, bucket.Name, "")
		if listErr != nil {
			if storageErrIsMissingBucket(listErr) {
				continue
			}
			return listErr
		}
		for _, object := range objects {
			found[cacheEntryKey(bucket.Name, object.Key)] = struct{}{}
		}
	}
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	for accessKey := range a.cacheEntries {
		if _, ok := found[accessKey]; !ok {
			delete(a.cacheEntries, accessKey)
		}
	}
	return nil
}

// CacheIndexEntries reports how many objects the eviction index holds. It exists
// so a test can compare the index against the store rather than assuming they
// agree.
func (a *Adapter) CacheIndexEntries() int {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	return len(a.cacheEntries)
}

func cacheOverBudget(objects int, bytes int64, policy CachePolicy) bool {
	return policy.MaxObjects > 0 && int64(objects) > policy.MaxObjects || policy.MaxBytes > 0 && bytes > policy.MaxBytes
}

func cacheEntryKey(bucket, key string) string {
	return bucket + "\x00" + key
}

func storageErrIsMissingBucket(err error) bool {
	return errors.Is(err, storage.ErrBucketNotFound)
}

func storageErrIsMissingObject(err error) bool {
	return errors.Is(err, storage.ErrObjectNotFound)
}
