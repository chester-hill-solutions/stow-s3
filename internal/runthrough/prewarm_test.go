package runthrough_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// Prewarm had no unit test at all. It was verified once by hand, end to end, and
// it shipped a report that claimed five keys were warm when the cache held two —
// which no amount of hand-verification catches, because the hand run warmed three
// keys into a cache that holds three and the two numbers agreed.
//
// The property these tests exist to pin is that the report is the cache. It is not
// a detail of the output format: the caller of a pre-warm is about to cut the
// network off, so a key reported as warm is a key somebody will read expecting it
// to be there.

// prewarmHarness is a run-through adapter with a separate cache and a byte or
// object budget the caller sets, so a test can make the cache smaller than the warm.
type prewarmHarness struct {
	adapter *runthrough.Adapter
	cache   storage.Store
	up      *mockUpstream
	ctx     context.Context
}

func newPrewarmHarness(t *testing.T, cache runthrough.CachePolicy) *prewarmHarness {
	t.Helper()
	ctx := context.Background()
	local, cacheStore := storage.NewMemoryStore(), storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create the local bucket: %v", err)
	}
	if err := cacheStore.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create the cache bucket: %v", err)
	}
	up := newMockUpstream()
	adapter := runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache,
		Cache:  cache,
	}, local, cacheStore, up)
	return &prewarmHarness{adapter: adapter, cache: cacheStore, up: up, ctx: ctx}
}

func (h *prewarmHarness) holds(t *testing.T, keys ...string) int {
	t.Helper()
	held := 0
	for _, key := range keys {
		if _, err := h.cache.HeadObject(h.ctx, "bucket", key); err == nil {
			held++
		}
	}
	return held
}

func cachedKeys(results []runthrough.PrewarmResult) []string {
	var keys []string
	for _, result := range results {
		if result.Cached {
			keys = append(keys, result.Key)
		}
	}
	return keys
}

// TestPrewarmReportsTheCacheAndNotTheFetch is the regression. A warm of five keys
// into a cache bounded to two objects must report two, because two is what the
// cache holds when the warm is over. The previous implementation reported the
// fetch — five — and the five-key figure was still true of a cache nobody could
// read from afterwards.
func TestPrewarmReportsTheCacheAndNotTheFetch(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{MaxObjects: 2})
	keys := []string{"k1", "k2", "k3", "k4", "k5"}
	for _, key := range keys {
		if _, err := h.up.PutObject(h.ctx, "bucket", key, bytes.NewReader([]byte("payload "+key)), storage.PutOptions{}); err != nil {
			t.Fatalf("seed %q: %v", key, err)
		}
	}
	results, err := h.adapter.Prewarm(h.ctx, "bucket", keys)
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	reported, held := len(cachedKeys(results)), h.holds(t, keys...)
	if reported != held {
		t.Errorf("the report says %d of %d keys are warm and the cache holds %d: a caller about to cut the network off is told %d keys are readable and only %d are.\nreported warm: %v",
			reported, len(keys), held, reported, held, cachedKeys(results))
	}
	if reported != 2 {
		t.Errorf("the cache is bounded to 2 objects, so the report should name 2, not %d: %v", reported, cachedKeys(results))
	}
}

// The report keeps the caller's order, so a caller can line it up against the list
// it wrote. The warm fetches concurrently, so completion order says nothing; only
// the index a result is written to can hold the order.
func TestPrewarmKeepsTheOrderTheCallerAskedIn(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{})
	keys := []string{"z", "m", "a", "q", "b", "c"}
	for _, key := range keys {
		if _, err := h.up.PutObject(h.ctx, "bucket", key, bytes.NewReader([]byte(key)), storage.PutOptions{}); err != nil {
			t.Fatalf("seed %q: %v", key, err)
		}
	}
	results, err := h.adapter.Prewarm(h.ctx, "bucket", keys)
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	if len(results) != len(keys) {
		t.Fatalf("the report has %d entries for %d keys", len(results), len(keys))
	}
	for i, key := range keys {
		if results[i].Key != key {
			t.Errorf("result %d is %q, not %q: the report lost the order the caller asked in", i, results[i].Key, key)
		}
	}
}

// A key the upstream does not have is reported, and the rest of the warm continues.
// One missing key must not abandon a warm of a thousand, and the caller has to be
// able to tell which ones are missing rather than being handed one aggregate error.
func TestPrewarmReportsAMissingKeyAndWarmsTheRest(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{})
	for _, key := range []string{"here", "also-here"} {
		if _, err := h.up.PutObject(h.ctx, "bucket", key, bytes.NewReader([]byte(key)), storage.PutOptions{}); err != nil {
			t.Fatalf("seed %q: %v", key, err)
		}
	}
	results, err := h.adapter.Prewarm(h.ctx, "bucket", []string{"here", "absent", "also-here"})
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	want := map[string]bool{"here": true, "absent": false, "also-here": true}
	for _, result := range results {
		if result.Cached != want[result.Key] {
			t.Errorf("%q: cached is %v, and the upstream has it: %v", result.Key, result.Cached, want[result.Key])
		}
		if result.Key == "absent" && result.Reason != "not-found" {
			t.Errorf("%q: reason is %q, and a key the upstream does not have should say so rather than reporting an empty reason", result.Key, result.Reason)
		}
	}
}

// Offline is a refusal, not a preference, and a pre-warm is nothing but an upstream
// call. Nothing is fetched, and the report says why for every key the cache does not
// already hold — but the report is the cache, and the cache is a local store, so an
// offline pre-warm still answers the only question worth asking once the network is
// off: what can I read right now.
func TestPrewarmOfflineFetchesNothingAndStillReportsTheCache(t *testing.T) {
	ctx := context.Background()
	local, cacheStore := storage.NewMemoryStore(), storage.NewMemoryStore()
	_ = local.CreateBucket(ctx, "bucket")
	_ = cacheStore.CreateBucket(ctx, "bucket")
	up := newMockUpstream()
	for _, key := range []string{"warm", "cold"} {
		if _, err := up.PutObject(ctx, "bucket", key, bytes.NewReader([]byte(key)), storage.PutOptions{}); err != nil {
			t.Fatalf("seed %q: %v", key, err)
		}
	}
	warming := runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache,
	}, local, cacheStore, up)
	if _, err := warming.Prewarm(ctx, "bucket", []string{"warm"}); err != nil {
		t.Fatalf("warm the cache first: %v", err)
	}
	offline := runthrough.NewWithCache(runthrough.Config{
		Policy:  runthrough.PolicyReadThroughCache,
		Offline: true,
	}, local, cacheStore, up)

	results, err := offline.Prewarm(ctx, "bucket", []string{"warm", "cold"})
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	if !results[0].Cached {
		t.Errorf("%q is already in the cache and an offline pre-warm reports it as not warm, so the one verb that can answer \"what can I read now\" answers \"nothing\"", results[0].Key)
	}
	if results[1].Cached {
		t.Errorf("%q: reported as warm, and nothing was fetched for it", results[1].Key)
	}
	if results[1].Reason != "offline" {
		t.Errorf("%q: reason is %q, and the adapter refused to reach for it", results[1].Key, results[1].Reason)
	}
	if up.getCalls != 1 {
		t.Errorf("the upstream was called %d times, and exactly one call was the warm that filled the cache: offline refused every call the pre-warm would have made", up.getCalls)
	}
}

// A cancelled warm reports every key it did not attempt. A key left at a zero value
// would report as a failure with no reason, which reads as a key the upstream did
// not have — a different answer, about a different thing, reached by doing nothing.
func TestPrewarmCancelledNamesTheKeysItDidNotAttempt(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{})
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		if _, err := h.up.PutObject(h.ctx, "bucket", key, bytes.NewReader([]byte(key)), storage.PutOptions{}); err != nil {
			t.Fatalf("seed %q: %v", key, err)
		}
	}
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	results, err := h.adapter.Prewarm(ctx, "bucket", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"})
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	if len(results) != 12 {
		t.Fatalf("the report has %d entries for 12 keys: a cancelled warm still reports every key it was given", len(results))
	}
	for _, result := range results {
		if result.Cached {
			t.Errorf("%q: reported as cached under a cancelled context", result.Key)
		}
		if result.Reason == "" {
			t.Errorf("%q: reported with no reason, which reads as a key the upstream did not have", result.Key)
		}
	}
}

// A warm with no bucket is a refusal, and it is reported per key rather than
// swallowed: the cache is a directory per bucket, so there is no sensible default,
// and a guessed bucket would warm the wrong one silently.
func TestPrewarmRefusesWithoutABucket(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{})
	results, err := h.adapter.Prewarm(h.ctx, "", []string{"a", "b"})
	if err == nil {
		t.Error("Prewarm with no bucket returned no error, and there is no bucket to warm")
	}
	if len(results) != 2 {
		t.Fatalf("the report has %d entries for 2 keys", len(results))
	}
	for _, result := range results {
		if result.Cached || result.Reason == "" {
			t.Errorf("%q: cached is %v with reason %q, and a warm with no bucket warms nothing", result.Key, result.Cached, result.Reason)
		}
	}
}

// The eviction hold is not only about the report. Per write, the cache re-plans
// eviction from the whole index, so a warm of N keys into a cache of M costs N
// walks and N sorts of M. A warm of 200 keys into a cache bounded to 20 should
// evict 180 objects once the fill is done, not re-plan 200 times to get there.
func TestPrewarmEvictsOnceRatherThanPerKey(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{MaxObjects: 20})
	keys := make([]string, 0, 200)
	for _, key := range []string{
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t",
	} {
		keys = append(keys, key)
	}
	for _, key := range keys {
		if _, err := h.up.PutObject(h.ctx, "bucket", key, bytes.NewReader([]byte(key)), storage.PutOptions{}); err != nil {
			t.Fatalf("seed %q: %v", key, err)
		}
	}
	results, err := h.adapter.Prewarm(h.ctx, "bucket", keys)
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	if reported := len(cachedKeys(results)); reported != 20 {
		t.Errorf("the report names %d warm keys and the cache is bounded to 20", reported)
	}
	if held := h.holds(t, keys...); held != 20 {
		t.Errorf("the cache holds %d objects and is bounded to 20", held)
	}
	// The index has to agree with the store afterwards, or the next eviction plans
	// against objects that are gone and the limit stops meaning anything.
	if entries := h.adapter.CacheIndexEntries(); entries != 20 {
		t.Errorf("the index holds %d entries and the cache holds 20: they have to agree or the limit is enforced against fiction", entries)
	}
	// A key the cache had to give up says so. "not in the cache" without a reason is
	// the same answer as a key the upstream never had, and those are different
	// problems for whoever is about to cut the network off.
	for _, result := range results {
		if result.Cached {
			continue
		}
		if result.Reason != "not in the cache: the limits did not leave room for it" {
			t.Errorf("%q: a key the limits evicted reports %q, and that is not the same as a key the upstream does not have", result.Key, result.Reason)
		}
	}
}

// A key the local store already holds was never a cache candidate, and the report
// says that rather than blaming the limits.
//
// Both are "not in the cache", and telling them apart is the difference between
// raising the cache and giving up on a key that was never going to be cached. The
// local store is the authority on which of the two it is: resolveObject answers from
// there without ever going near the cache, so the fetch looks like it succeeded and
// the key still is not in the cache. Without the local check the only available
// reason is the limits', and it would be wrong every time.
func TestPrewarmSaysAKeyTheLocalStoreHeldWasNeverACacheCandidate(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{})
	// Seed the upstream too, so a key that is only local is a deliberate arrangement
	// rather than a coincidence: the local store is authoritative and is answered
	// from first, so this is what the case looks like either way.
	if _, err := h.up.PutObject(h.ctx, "bucket", "local-only", bytes.NewReader([]byte("x")), storage.PutOptions{}); err != nil {
		t.Fatalf("seed the upstream: %v", err)
	}
	if _, err := h.adapter.PutObject(h.ctx, "bucket", "local-only", bytes.NewReader([]byte("x")), storage.PutOptions{}); err != nil {
		t.Fatalf("seed the local store: %v", err)
	}

	results, err := h.adapter.Prewarm(h.ctx, "bucket", []string{"local-only"})
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	if results[0].Cached {
		t.Error("a key that is only in the local store is reported as cached, and cached means in the cache")
	}
	if !strings.Contains(results[0].Reason, "local store") {
		t.Errorf("reason is %q, and a key the local store held is not a key the limits dropped", results[0].Reason)
	}
}

// A key whose lifetime has already run reads as a miss, not as a warm key with a// caveat. "Cached" has to mean readable, because the caller acts on it by cutting
// the network off.
func TestPrewarmReportsAnExpiredEntryAsAMiss(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{TTL: time.Nanosecond})
	if _, err := h.up.PutObject(h.ctx, "bucket", "k", bytes.NewReader([]byte("k")), storage.PutOptions{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	time.Sleep(time.Millisecond)
	results, err := h.adapter.Prewarm(h.ctx, "bucket", []string{"k"})
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("the report has %d entries for 1 key", len(results))
	}
	if results[0].Cached {
		t.Error("a key whose lifetime has passed is reported as cached, and cached has to mean readable")
	}
	if results[0].Reason == "" {
		t.Error("a key reported as not cached has no reason")
	}
}

// A key the upstream cannot serve is reported as an error rather than as a miss
// with no detail, so a caller can tell a missing key from a broken upstream. Both
// are "not warm", and only one of them is the upstream's answer.
func TestPrewarmDistinguishesAnUpstreamFailureFromAMissingKey(t *testing.T) {
	h := newPrewarmHarness(t, runthrough.CachePolicy{})
	if _, err := h.up.PutObject(h.ctx, "bucket", "broken", bytes.NewReader([]byte("b")), storage.PutOptions{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h.up.getErrors[objectKey("bucket", "broken")] = errors.New("the upstream said no")
	results, err := h.adapter.Prewarm(h.ctx, "bucket", []string{"broken"})
	if err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("the report has %d entries for 1 key", len(results))
	}
	if results[0].Cached {
		t.Error("a key whose fetch failed is reported as cached")
	}
	if results[0].Reason == "not-found" {
		t.Errorf("a failed fetch is reported as %q, which is the upstream's answer for a key it does not have rather than a failure to ask", results[0].Reason)
	}
}
