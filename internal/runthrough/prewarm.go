package runthrough

import (
	"context"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// PrewarmResult is what one pre-warm attempt did.
type PrewarmResult struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	// Cached is true when the object is readable with the network gone after this
	// call, whether it was fetched now or was already there.
	Cached bool  `json:"cached"`
	Bytes  int64 `json:"bytes"`
	// Reason explains a miss, and is empty on success. "not-found" means the
	// upstream does not have the key, "offline" means the adapter refused to reach
	// for it, and "error: …" means the fetch failed.
	Reason string `json:"reason,omitempty"`
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
func (a *Adapter) Prewarm(ctx context.Context, bucket string, keys []string) []PrewarmResult {
	results := make([]PrewarmResult, 0, len(keys))
	if bucket == "" {
		// A bucket is required rather than defaulted. The cache is a directory per
		// bucket and the adapter is configured for one upstream bucket, so there is
		// no sensible guess, and guessing would warm the wrong one silently.
		for _, key := range keys {
			results = append(results, PrewarmResult{Key: key, Reason: "a bucket is required"})
		}
		return results
	}
	for _, key := range keys {
		results = append(results, a.prewarmOne(ctx, bucket, key))
	}
	return results
}

func (a *Adapter) prewarmOne(ctx context.Context, bucket, key string) PrewarmResult {
	result := PrewarmResult{Bucket: bucket, Key: key}
	if err := ctx.Err(); err != nil {
		result.Reason = "cancelled: " + err.Error()
		return result
	}
	if a.cfg.Offline {
		// Offline refuses every upstream call, and a pre-warm is nothing but one.
		// Reporting it as a miss rather than a silent no-op is what lets a caller
		// tell "the network is off, warm nothing" from "everything warmed".
		result.Reason = "offline"
		return result
	}
	if _, _, err := a.resolveObject(ctx, bucket, key, true); err != nil {
		switch {
		case errors.Is(err, storage.ErrObjectNotFound):
			result.Reason = "not-found"
		default:
			result.Reason = "error: " + err.Error()
		}
		return result
	}
	// Confirm it landed, rather than reporting success from the absence of an
	// error. A cache that is not separate has no cache to warm, and saying "cached"
	// there would be a claim about a store the caller cannot see.
	if !a.separateCache {
		result.Reason = "no separate cache: the local store already holds it"
		result.Cached = true
		return result
	}
	meta, err := a.cache.HeadObject(ctx, bucket, key)
	if err != nil {
		result.Reason = "error: the object was fetched but is not in the cache: " + err.Error()
		return result
	}
	result.Cached = !a.cacheEntryExpired(bucket, key)
	if !result.Cached {
		result.Reason = "cached with a lifetime that has already passed"
	}
	result.Bytes = meta.Size
	return result
}
