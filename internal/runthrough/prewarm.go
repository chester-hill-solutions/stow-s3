package runthrough

import (
	"context"
	"errors"
	"sync"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// prewarmConcurrency bounds this function's own fan-out against the upstream, so a
// long key list does not become a long list of simultaneous connections.
const prewarmConcurrency = 8

type PrewarmResult struct {
	Bucket string
	Key    string
	// Cached is true when the object is in the cache once this call has finished, not
	// when it was fetched: the limits apply while a warm is filling, so a key warmed
	// early can be pushed out by one warmed later.
	Cached bool
	Bytes  int64
	// Reason explains a miss, and is empty on success. The causes are kept apart
	// because a caller can act on each: the bucket's answer, the adapter's refusal,
	// and the cache's own three.
	Reason string
}

// Prewarm fetches the named keys into the cache so they are readable with the
// network gone.
//
// It takes an explicit key list and never a prefix: a prefix on a real bucket is a
// data-exfiltration shape, and this function's caller is an agent that reads
// untrusted text. A key that cannot be warmed is reported and the rest continue, so
// the caller sees which ones are missing rather than one aggregate failure.
//
// It runs in two phases — fetch everything, then report what the cache holds. The
// error is the limits not being enforceable, not a per-key outcome: a key that
// could not be fetched is an answer, and a cache that grew past its bound is a
// fault.
func (a *Adapter) Prewarm(ctx context.Context, bucket string, keys []string) ([]PrewarmResult, error) {
	if bucket == "" {
		// The cache is a directory per bucket and the adapter is configured for one
		// upstream bucket, so there is no sensible default here.
		results := make([]PrewarmResult, len(keys))
		for i, key := range keys {
			results[i] = PrewarmResult{Key: key, Reason: "a bucket is required"}
		}
		return results, errors.New("prewarm requires a bucket")
	}
	attempts := make([]prewarmAttempt, len(keys))
	a.holdEvictions()
	a.fetchKeys(ctx, bucket, keys, attempts)
	if err := a.releaseEvictions(ctx); err != nil {
		return nil, err
	}
	return a.reportWarmed(ctx, bucket, keys, attempts), nil
}

// prewarmAttempt is the outcome of reaching for one key. fetched is not yet warm,
// because the limits have not been applied yet.
type prewarmAttempt struct {
	// A key the adapter refused to reach for was not fetched and is still asked
	// about, because an offline pre-warm should report what the cache already has.
	fetched bool
	// reason is empty exactly when fetched is true. fetchKeys relies on that pairing
	// to recognise a key the cancellation loop never got to, and the zero value is
	// the one attempt neither a fetch nor a refusal can produce.
	reason string
}

// Writing by index is what keeps the report in the caller's order.
func (a *Adapter) fetchKeys(ctx context.Context, bucket string, keys []string, attempts []prewarmAttempt) {
	if a.cfg.Offline {
		// Nothing is fetched, and the keys are still reported from the local cache.
		for i := range attempts {
			attempts[i] = prewarmAttempt{reason: "offline"}
		}
		return
	}

	queue := make(chan int)
	var waiting sync.WaitGroup
	workers := min(prewarmConcurrency, len(keys))
	waiting.Add(workers)
	for range workers {
		go func() {
			defer waiting.Done()
			for index := range queue {
				attempts[index] = a.fetchOne(ctx, bucket, keys[index])
			}
		}()
	}
	for index := range keys {
		if ctx.Err() != nil {
			break
		}
		queue <- index
	}
	close(queue)
	waiting.Wait()

	// The producer stops handing out work at the first cancellation, so the keys it
	// never queued are still at their zero value, which would report as a key that
	// failed with no reason at all.
	for i := range attempts {
		if attempts[i] == (prewarmAttempt{}) {
			attempts[i] = prewarmAttempt{reason: "cancelled: " + ctx.Err().Error()}
		}
	}
}

// fetchOne reports the attempt rather than the outcome: whether the key ends up warm
// is not knowable until the fill has settled.
func (a *Adapter) fetchOne(ctx context.Context, bucket, key string) prewarmAttempt {
	if _, _, err := a.resolveObject(ctx, bucket, key, true); err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return prewarmAttempt{reason: "not-found"}
		}
		return prewarmAttempt{reason: "error: " + err.Error()}
	}
	return prewarmAttempt{fetched: true}
}

// The limits have been applied by the time this runs, so a key a later fetch pushed
// out reads as a miss rather than as a success.
func (a *Adapter) reportWarmed(ctx context.Context, bucket string, keys []string, attempts []prewarmAttempt) []PrewarmResult {
	results := make([]PrewarmResult, len(keys))
	for i, key := range keys {
		results[i] = a.reportWarmedKey(ctx, bucket, key, attempts[i])
	}
	return results
}

// The cache is a local store, so this is a local read whether or not a fetch
// happened.
func (a *Adapter) reportWarmedKey(ctx context.Context, bucket, key string, attempt prewarmAttempt) PrewarmResult {
	result := PrewarmResult{Bucket: bucket, Key: key}
	// A cache that is not separate has no cache to warm, so a fetch is the only
	// evidence available for saying "cached".
	if !a.separateCache {
		if attempt.fetched {
			result.Cached = true
			result.Reason = "no separate cache: the local store already holds it"
			return result
		}
		result.Reason = attempt.reason
		return result
	}

	meta, err := a.cache.HeadObject(ctx, bucket, key)
	if err != nil {
		if !storageErrIsMissingObject(err) && !storageErrIsMissingBucket(err) {
			result.Reason = "error: the cache could not be read: " + err.Error()
			return result
		}
		// Not in the cache, and the attempt does not say why: either the local store
		// held it and it was never a cache candidate, or a limit gave it up. The local
		// store is the only one that can tell them apart, and they call for opposite
		// responses — raise the cache, or stop asking.
		result.Reason = attempt.reason
		if result.Reason == "" {
			result.Reason = a.reasonNotInCache(ctx, bucket, key)
		}
		return result
	}
	result.Bytes = meta.Size
	if a.cacheEntryExpired(bucket, key) {
		result.Reason = "cached with a lifetime that has already passed"
		return result
	}
	result.Cached = true
	return result
}

func (a *Adapter) reasonNotInCache(ctx context.Context, bucket, key string) string {
	if _, err := a.local.HeadObject(ctx, bucket, key); err == nil {
		return "held in the local store, so it was never a cache candidate"
	}
	return "not in the cache: the limits did not leave room for it"
}
