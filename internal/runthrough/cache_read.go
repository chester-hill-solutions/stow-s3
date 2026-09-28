package runthrough

import (
	"context"
	"errors"
	"io"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// Serving a read once the local store has missed.
//
// This is the cache half of resolveObject, split into its own file because it is a
// unit with a shape of its own: a cache hit, a cache hit needing revalidation, and
// the miss that reaches upstream. The three live together because the decision
// between them is one decision, and separating them across a file boundary would
// hide the ordering that makes an offline read work.

func (a *Adapter) resolveCachedObject(ctx context.Context, bucket, key string, needBody bool) (io.ReadCloser, *storage.ObjectMeta, error) {
	cacheMeta, cacheErr := a.cache.HeadObject(ctx, bucket, key)
	if cacheErr == nil && a.cacheEntryExpired(bucket, key) {
		if err := a.cache.DeleteObject(ctx, bucket, key); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
			return nil, nil, err
		}
		a.forgetCacheObject(bucket, key)
		a.cacheEvictions.Add(1)
		cacheMeta, cacheErr = nil, storage.ErrObjectNotFound
	}
	if cacheErr == nil {
		// Offline serves the cache as it stands. Revalidation is an upstream call,
		// so there is nothing to revalidate against, and a cached object past its
		// TTL has already been dropped above rather than served stale.
		if a.cfg.Offline || !a.cfg.Revalidate {
			return a.openCached(ctx, bucket, key, cacheMeta, needBody)
		}
		return a.revalidateCachedObject(ctx, bucket, key, cacheMeta, needBody)
	}
	// A separately configured cache starts empty and does not need bucket
	// scaffolding. Treat a missing cache bucket as an empty cache; the local
	// bucket remains the authoritative namespace and is checked above.
	if !storageErrIsMissingObject(cacheErr) && !storageErrIsMissingBucket(cacheErr) {
		return nil, nil, cacheErr
	}
	if a.cfg.Offline {
		// Not a miss the caller can retry into: the object is not here and the
		// network is not an option. NoSuchKey says exactly that.
		return nil, nil, storage.ErrObjectNotFound
	}
	return a.refreshFromUpstream(ctx, bucket, key, needBody)
}

func (a *Adapter) revalidateCachedObject(ctx context.Context, bucket, key string, cachedMeta *storage.ObjectMeta, needBody bool) (io.ReadCloser, *storage.ObjectMeta, error) {
	upMeta, headErr := a.upstream.HeadObject(ctx, bucket, key)
	if headErr == storage.ErrObjectNotFound {
		if a.cfg.EvictOnUpstreamMissing {
			_ = a.cache.DeleteObject(ctx, bucket, key)
			a.forgetCacheObject(bucket, key)
			if !a.separateCache {
				_ = a.local.DeleteObject(ctx, bucket, key)
			}
		}
		return nil, nil, storage.ErrObjectNotFound
	}
	if headErr != nil {
		return a.openCached(ctx, bucket, key, cachedMeta, needBody)
	}
	if !upstreamChanged(upMeta, cachedMeta) {
		return a.openCached(ctx, bucket, key, cachedMeta, needBody)
	}
	return a.refreshFromUpstream(ctx, bucket, key, needBody)
}

func (a *Adapter) openLocal(ctx context.Context, bucket, key string, meta *storage.ObjectMeta, needBody bool) (io.ReadCloser, *storage.ObjectMeta, error) {
	if !needBody {
		return nil, meta, nil
	}
	rc, bodyMeta, err := a.local.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	return rc, bodyMeta, nil
}

func (a *Adapter) openCached(ctx context.Context, bucket, key string, meta *storage.ObjectMeta, needBody bool) (io.ReadCloser, *storage.ObjectMeta, error) {
	a.cacheHits.Add(1)
	a.touchCache(bucket, key)
	if !needBody {
		return nil, meta, nil
	}
	store := a.local
	if a.separateCache {
		store = a.cache
	}
	return store.GetObject(ctx, bucket, key)
}
