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
	held := a.evictionsHeld > 0
	a.cacheMu.Unlock()
	if held {
		return nil
	}
	return a.evictCache(ctx)
}

// holdEvictions suspends per-write eviction until the matching releaseEvictions.
//
// A write normally evicts immediately, and that is what makes a limit hold at all
// times. A bulk fill is the one case where the immediate shape is wrong twice over.
// It re-plans the whole index on every write — a walk and a sort of every cached
// object — so warming N keys into a cache of M costs N sorts of M, which is the
// dominant cost of warming a large cache. And a warm's own output is read while the
// fill is still running, so a key warmed early can be pushed out by a key warmed
// later, and a report taken per key claims keys are warm that the cache is about to
// disagree about. See Prewarm, which is the one caller.
func (a *Adapter) holdEvictions() {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.evictionsHeld++
}

// releaseEvictions resumes eviction and applies the limits once to everything
// accumulated while they were held.
//
// The hold is counted rather than a boolean so two overlapping fills cannot resume
// eviction while the other is still filling, which would reintroduce exactly the
// per-write plan the hold exists to avoid.
func (a *Adapter) releaseEvictions(ctx context.Context) error {
	a.cacheMu.Lock()
	a.evictionsHeld--
	held := a.evictionsHeld > 0
	a.cacheMu.Unlock()
	if held {
		return nil
	}
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
// HeadObject per object to recover the access time and size, so a cache of N objects
// took N round trips each time anything in it changed. The index already holds access
// time, expiry and size, so the same plan costs one pass over a map.
//
// What this gives up is discovery of cache objects with no index entry. Every path that
// populates the cache calls trackCacheObject, and reconcileCacheIndex rebuilds the index
// when that assumption is worth checking.
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
// ReconcileCacheIndex rebuilds the eviction index from the cache store, in both
// directions. Exported so the drift check is a test rather than a claim, and
// called at startup so a server over an existing cache directory is not
// enforcing its limits against an index that starts empty.
func (a *Adapter) ReconcileCacheIndex(ctx context.Context) error { return a.reconcileCacheIndex(ctx) }

// reconcileCacheIndex brings the index into agreement with the cache store in both
// directions: it drops entries for objects the store no longer has, and it adds
// entries for objects the store has and the index has never seen.
//
// The second half is what decides whether a restart is safe: the cache lives in a
// directory and this index in memory, so a server coming up over an existing cache
// holds bytes it knows nothing about, and every eviction plan is computed from the
// index alone.
func (a *Adapter) reconcileCacheIndex(ctx context.Context) error {
	buckets, err := a.cache.ListBuckets(ctx)
	if err != nil {
		return err
	}
	found := make(map[string]storage.ObjectMeta)
	for _, bucket := range buckets {
		objects, listErr := listAllObjects(ctx, a.cache, bucket.Name, "")
		if listErr != nil {
			if storageErrIsMissingBucket(listErr) {
				continue
			}
			return listErr
		}
		for _, object := range objects {
			found[cacheEntryKey(bucket.Name, object.Key)] = object
		}
	}
	a.cacheMu.Lock()
	for accessKey := range a.cacheEntries {
		if _, ok := found[accessKey]; !ok {
			delete(a.cacheEntries, accessKey)
		}
	}
	for accessKey, object := range found {
		if _, ok := a.cacheEntries[accessKey]; ok {
			continue
		}
		accessed := object.LastModified
		if accessed.IsZero() {
			accessed = time.Now()
		}
		a.cacheEntries[accessKey] = cacheEntry{
			accessedAt: accessed,
			bucket:     object.Bucket,
			key:        object.Key,
			size:       object.Size,
		}
	}
	a.cacheMu.Unlock()

	// Then apply the limits, once, to what was just discovered.
	//
	// Eviction is otherwise reached from exactly one place: trackCacheObject, which
	// only runs when this process writes to the cache. A server that came up over a
	// cache already larger than its bound and then only reads never evicts and never
	// converges, and a detached session spends its life reading.
	//
	// This runs once at startup rather than per read, because the alternative is an
	// eviction re-plan per request.
	return a.evictCache(ctx)
}

// CachedObject is one object the cache currently holds.
//
// It carries no JSON tags. A store-layer type is not a wire shape, and the surface
// that reports these renders them through its own type — the same arrangement the
// outbox inspection already uses. The tags were here so the admin route could
// marshal the struct directly, which made the HTTP contract a property of this
// package that no one reading this file would expect to be responsible for.
type CachedObject struct {
	Bucket    string
	Key       string
	Size      int64
	Accessed  time.Time
	ExpiresAt time.Time
	// Readable says whether this object can be served with the network gone.
	//
	// It is false for an object the upstream has moved past, and it is the whole
	// point of the listing: a cache that reports only counters tells an agent that
	// caching is happening and not what it can actually read, so an agent working
	// offline has to guess and then discover the answer by failing.
	Readable bool
}

// CachedObjects lists what the cache holds, oldest access first.
//
// It answers the question a detached session actually asks — what can I read
// without the network — and it is the index rather than a fresh store walk, so it
// is what the eviction policy is reasoning about. A listing built from the store
// would disagree with the policy the moment the two drifted, and the disagreement
// would be invisible.
func (a *Adapter) CachedObjects() []CachedObject {
	if !a.separateCache {
		return nil
	}
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	now := time.Now()
	objects := make([]CachedObject, 0, len(a.cacheEntries))
	for _, entry := range a.cacheEntries {
		objects = append(objects, CachedObject{
			Bucket: entry.bucket, Key: entry.key, Size: entry.size,
			Accessed: entry.accessedAt, ExpiresAt: entry.expiresAt,
			Readable: entry.expiresAt.IsZero() || now.Before(entry.expiresAt),
		})
	}
	// Oldest first: an agent about to be cut off wants the keys least likely to
	// still be there, and an operator wants the eviction order.
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].Accessed.Equal(objects[j].Accessed) {
			return objects[i].Key < objects[j].Key
		}
		return objects[i].Accessed.Before(objects[j].Accessed)
	})
	return objects
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
