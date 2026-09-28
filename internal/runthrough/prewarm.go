package runthrough

import (
	"context"
	"errors"
	"sync"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// prewarmConcurrency is how many keys are fetched at once.
//
// A pre-warm is the one operation here that is bulk by construction, and it runs on
// the way to cutting an agent off, which is where sequential fetches are felt
// rather than merely measured: five hundred keys at one round trip each is a wait
// long enough that an operator interrupts and starts again.
//
// It is a constant rather than a flag because what it bounds is this function's own
// fan-out against an upstream the operator configured, and a knob with no reason to
// turn is a knob that gets turned once. The bound is here to stop a long key list
// from becoming a long list of simultaneous connections.
const prewarmConcurrency = 8

// PrewarmResult is what one pre-warm attempt did.
type PrewarmResult struct {
	Bucket string
	Key    string
	// Cached is true when the object is in the cache once this call has finished,
	// whether it was fetched now or was already there.
	//
	// "In the cache, once this call has finished" is the whole claim, and it is why
	// the report reads the cache rather than recalling the fetch. The cache's limits
	// apply while a warm is filling it, so a key warmed early can be pushed out by a
	// key warmed later. Reported per key as it lands, a warm of five into a cache
	// that holds two claims all five are warm, and a caller about to cut the network
	// off is wrong about three of them — with an exit status of success.
	Cached bool
	Bytes  int64
	// Reason explains a miss, and is empty on success. The three causes are kept
	// apart because a caller can act on each: "not-found" is the upstream's answer,
	// "offline" is the adapter's refusal to ask, and the rest are the cache's — a
	// limit that had no room, a lifetime that has run, or a key the local store held
	// all along, which was never a cache candidate to begin with.
	Reason string
}

// Prewarm fetches the named keys into the cache so they are readable with the
// network gone.
//
// It takes an explicit key list and never a prefix. That is the load-bearing
// decision: a prefix on a real bucket is a data-exfiltration shape, and this
// function's caller is an agent that reads untrusted text. An agent handed
// "prefetch everything under logs/" would be handed a way to copy a bucket it was
// never given, and the operator who wrote the prefix would have no idea the
// distinction mattered. An explicit list is something a person or a policy chose.
//
// A key that cannot be warmed is reported and the rest continue. One missing key
// should not abandon a warm-up of a thousand, and the caller needs to know which
// ones are not there rather than a single aggregate failure.
//
// It runs in two phases — fetch everything, then report what the cache holds — and
// the error is the limits not being enforceable rather than a per-key outcome. A
// key that could not be fetched is an answer; a cache that grew past its bound
// because eviction failed is a fault, and mixing the two would let it pass as
// "four of five keys warmed".
func (a *Adapter) Prewarm(ctx context.Context, bucket string, keys []string) ([]PrewarmResult, error) {
	if bucket == "" {
		// A bucket is required rather than defaulted. The cache is a directory per
		// bucket and the adapter is configured for one upstream bucket, so there is
		// no sensible guess, and guessing would warm the wrong one silently.
		results := make([]PrewarmResult, len(keys))
		for i, key := range keys {
			results[i] = PrewarmResult{Key: key, Reason: "a bucket is required"}
		}
		return results, errors.New("prewarm requires a bucket")
	}
	attempts := make([]prewarmAttempt, len(keys))
	// Eviction is held for the whole fill and applied once, below, to the finished
	// cache. See holdEvictions: per write it re-plans the entire index, and it
	// changes under the report this function is about to write.
	a.holdEvictions()
	a.fetchKeys(ctx, bucket, keys, attempts)
	if err := a.releaseEvictions(ctx); err != nil {
		return nil, err
	}
	return a.reportWarmed(ctx, bucket, keys, attempts), nil
}

// prewarmAttempt is the outcome of reaching for one key, before the cache has
// settled.
//
// Fetched is not yet warm. Whether a fetched key is still in the cache is not
// knowable until every other key has been fetched and the limits applied, which is
// why this is a separate type from PrewarmResult rather than a field of it.
type prewarmAttempt struct {
	// fetched is true when the adapter reached for the key. A key the adapter refused
	// to reach for was not fetched and is still asked about, because the cache is a
	// local store: an offline pre-warm reports what is already there, which is the
	// only question worth answering once the network is off.
	fetched bool
	// reason explains an attempt that never reached the cache: empty when the key was
	// fetched, set otherwise. fetchKeys relies on that pairing to recognise a key the
	// cancellation loop never got to, and a zero attempt is the one value neither a
	// fetch nor a refusal can produce.
	reason string
}

// fetchKeys fills attempts[i] for keys[i], at most prewarmConcurrency at a time.
//
// Attempts are written by index, so the report keeps the order the caller asked in
// and a caller can line it up against the list it wrote.
func (a *Adapter) fetchKeys(ctx context.Context, bucket string, keys []string, attempts []prewarmAttempt) {
	if a.cfg.Offline {
		// Offline refuses every upstream call, and a pre-warm is nothing but one. The
		// keys are still reported — from the cache, which is a local store — so an
		// offline pre-warm answers "what can I read now", which is the only question
		// worth asking once the network is off. It is decided once here rather than
		// per key because it is a property of the adapter, not of a key.
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
	// never queued are still at their zero value. They are named here rather than
	// left alone, because a zero attempt would report as a key that failed with no
	// reason at all — which reads as a key the upstream did not have.
	for i := range attempts {
		if attempts[i] == (prewarmAttempt{}) {
			attempts[i] = prewarmAttempt{reason: "cancelled: " + ctx.Err().Error()}
		}
	}
}

// fetchOne reaches for a key and reports the attempt. It deliberately does not
// report whether the key is warm: that is not knowable until the fill has settled.
func (a *Adapter) fetchOne(ctx context.Context, bucket, key string) prewarmAttempt {
	if _, _, err := a.resolveObject(ctx, bucket, key, true); err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return prewarmAttempt{reason: "not-found"}
		}
		return prewarmAttempt{reason: "error: " + err.Error()}
	}
	return prewarmAttempt{fetched: true}
}

// reportWarmed reads the settled cache and reports what is actually in it. The
// limits have been applied by the time this runs, so a key a later fetch pushed out
// reads as a miss here instead of as a success the caller discovers by failing
// offline.
func (a *Adapter) reportWarmed(ctx context.Context, bucket string, keys []string, attempts []prewarmAttempt) []PrewarmResult {
	results := make([]PrewarmResult, len(keys))
	for i, key := range keys {
		results[i] = a.reportWarmedKey(ctx, bucket, key, attempts[i])
	}
	return results
}

// reportWarmedKey answers for one key by asking the cache, not by recalling the
// fetch.
//
// The cache is a local store, so this is a local read whether or not a fetch
// happened, and it is the only thing that can say what a caller will actually find
// once the network is gone. Asking it for every key is also what makes an offline
// pre-warm worth running: the adapter refuses to reach upstream, so the report is
// the contents of the cache, which is the whole question.
func (a *Adapter) reportWarmedKey(ctx context.Context, bucket, key string, attempt prewarmAttempt) PrewarmResult {
	result := PrewarmResult{Bucket: bucket, Key: key}
	// A cache that is not separate has no cache to warm, and saying "cached" there
	// would be a claim about a store the caller cannot see. A fetch is the evidence
	// there, and without one there is nothing to report but the reason.
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
		// The key is not in the cache and the attempt does not say why, because there
		// are two reasons and they need telling apart. Either the local store already
		// held it — in which case the read never went near the cache and the key was
		// never a cache candidate — or the key was fetched and a limit gave it up.
		// Only the local store can answer which, and it is one head on a key already
		// known to be missing, so the answer is cheap and it is the difference
		// between "raise the cache" and "that key was never going to be cached".
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

// reasonNotInCache names why a fetched key is not in the cache, by asking the one
// store that can tell.
func (a *Adapter) reasonNotInCache(ctx context.Context, bucket, key string) string {
	if _, err := a.local.HeadObject(ctx, bucket, key); err == nil {
		return "held in the local store, so it was never a cache candidate"
	}
	return "not in the cache: the limits did not leave room for it"
}
