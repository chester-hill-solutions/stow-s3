package s3api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The inspection route already reported cache hits, misses and evictions. Those
// three numbers say that caching is happening; they say nothing about what is
// cached, which is the question an agent cut off from the network is actually
// asking. A cache can post a perfect hit rate and hold nothing that agent wanted,
// and there was no way to tell those two situations apart from outside.
//
// These tests pin the listing that answers it, and the boundaries around it.

// cacheListingStore is a store that reports a fixed set of cached objects, so the
// route's paging and filtering are tested without standing up a run-through cache.
// It embeds the memory store so every other operation the inspection performs —
// listing buckets, walking objects, reading multipart state — behaves normally, and
// the assertions are about the listing rather than about a stub that happens to
// satisfy one code path.
type cacheListingStore struct {
	storage.Store
	objects []runthrough.CachedObject
}

func (s cacheListingStore) CachedObjects() []runthrough.CachedObject { return s.objects }

// inspectPayload decodes the response and returns the two things every assertion
// here is about: the cached key listing, and the count beside it.
//
// Decoding into a struct rather than a map is deliberate. These tests are about a
// document's shape, and a map lookup that misses returns nil, which compares
// unequal to everything — so a renamed JSON field produces a failure that names the
// wrong thing. A struct field that disappears fails to compile.
type inspectPayload struct {
	CachedKeys  []cachedKey `json:"cached_keys"`
	CachedCount int         `json:"cached_key_count"`
}

type cachedKey struct {
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
	Size      int64     `json:"size"`
	Readable  bool      `json:"readable"`
	ExpiresAt time.Time `json:"expires_at"`
	HasExpiry bool      `json:"has_expiry"`
}

func inspect(t *testing.T, store storage.Store, query string) inspectPayload {
	t.Helper()
	recorder := httptest.NewRecorder()
	server := &Server{store: store}
	request := httptest.NewRequest(http.MethodGet, "/_stow/inspect"+query, nil)
	server.writeInspect(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("inspect%s: status %d, body %s", query, recorder.Code, recorder.Body.String())
	}
	var payload inspectPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("inspect%s: response is not the expected shape: %v\n%s", query, err, recorder.Body.String())
	}
	return payload
}

func keysOf(payload inspectPayload) []string {
	keys := make([]string, 0, len(payload.CachedKeys))
	for _, object := range payload.CachedKeys {
		keys = append(keys, object.Key)
	}
	return keys
}

func threeCachedObjects() []runthrough.CachedObject {
	return []runthrough.CachedObject{
		{Bucket: "datasets", Key: "models/a.bin", Size: 10, Readable: true},
		{Bucket: "datasets", Key: "models/b.bin", Size: 20, Readable: true},
		{Bucket: "other", Key: "z.bin", Size: 30, Readable: false},
	}
}

func listingStore(objects []runthrough.CachedObject) storage.Store {
	return cacheListingStore{Store: storage.NewMemoryStore(), objects: objects}
}

func TestInspectNamesTheCachedKeysAndNotOnlyTheCounters(t *testing.T) {
	payload := inspect(t, listingStore(threeCachedObjects()), "")

	keys := keysOf(payload)
	want := []string{"models/a.bin", "models/b.bin", "z.bin"}
	if len(keys) != len(want) {
		t.Fatalf("the listing named %d keys, want %d: %v", len(keys), len(want), keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("key %d is %q, want %q: %v", i, keys[i], want[i], keys)
		}
	}
	if payload.CachedCount != len(want) {
		t.Fatalf("the listing reports %d keys in total, want %d", payload.CachedCount, len(want))
	}
}

func TestInspectSaysWhichCachedKeysAreReadable(t *testing.T) {
	// `readable` is the reason the listing exists. An object the upstream has moved
	// past is in the cache and not readable, and an agent that cannot tell those
	// apart will try it and fail.
	payload := inspect(t, listingStore(threeCachedObjects()), "")

	readable := map[string]bool{}
	for _, object := range payload.CachedKeys {
		readable[object.Key] = object.Readable
	}
	if !readable["models/a.bin"] {
		t.Error("a readable key is reported as unreadable")
	}
	if readable["z.bin"] {
		t.Error("an unreadable key is reported as readable")
	}
}

// has_expiry is the boolean beside a zero timestamp, and it is here because
// `omitempty` does not omit a zero time.Time.
//
// An agent reading a serialised zero could not tell a cache entry that never
// expires from one that expired at the beginning of time. The same shape as
// has_ttl beside expires_in_seconds, for the same reason: a zero that means two
// things is not a value.
func TestInspectSaysWhetherACachedKeyHasALifetimeAtAll(t *testing.T) {
	expiring := time.Now().Add(time.Hour).Round(time.Second)
	payload := inspect(t, listingStore([]runthrough.CachedObject{
		{Bucket: "datasets", Key: "forever", Size: 1, Readable: true},
		{Bucket: "datasets", Key: "expires", Size: 1, Readable: true, ExpiresAt: expiring},
	}), "")

	byKey := map[string]cachedKey{}
	for _, object := range payload.CachedKeys {
		byKey[object.Key] = object
	}
	if entry, present := byKey["forever"]; !present {
		t.Fatalf("the listing does not name the entry with no lifetime: %v", byKey)
	} else if entry.HasExpiry {
		t.Error("an entry with no lifetime reports has_expiry, so a caller cannot tell it from one that expires")
	}
	if entry, present := byKey["expires"]; !present {
		t.Fatalf("the listing does not name the entry with a lifetime: %v", byKey)
	} else {
		if !entry.HasExpiry {
			t.Error("an entry with a lifetime reports has_expiry false, so its expires_at looks like the no-lifetime case")
		}
		if !entry.ExpiresAt.Equal(expiring) {
			t.Errorf("expires_at is %v, want the entry's own %v", entry.ExpiresAt, expiring)
		}
	}
}

func TestInspectFiltersTheListingByBucketAndPrefix(t *testing.T) {
	store := listingStore(threeCachedObjects())
	byBucket := keysOf(inspect(t, store, "?bucket=datasets"))
	if len(byBucket) != 2 {
		t.Fatalf("filtering by bucket returned %v, want the two datasets keys", byBucket)
	}
	byPrefix := keysOf(inspect(t, store, "?prefix=models/"))
	if len(byPrefix) != 2 {
		t.Fatalf("filtering by prefix returned %v, want the two model keys", byPrefix)
	}
	// The total is the whole cache, not the filtered page: an agent asking about
	// one bucket still needs to know the cache is not bigger than what it can see.
	total := inspect(t, store, "?bucket=datasets").CachedCount
	if total != 3 {
		t.Fatalf("a filtered listing reports a total of %v, want the whole cache's 3", total)
	}
}

func TestInspectPagesTheListingAndSaysTheTotalAnyway(t *testing.T) {
	// A page with no total is indistinguishable from a cache holding exactly that
	// much, which is the difference between "you can read all of it" and "you are
	// missing four keys".
	payload := inspect(t, listingStore(threeCachedObjects()), "?limit=1")

	if keys := keysOf(payload); len(keys) != 1 {
		t.Fatalf("a limit of 1 returned %d keys", len(keys))
	}
	if payload.CachedCount != 3 {
		t.Fatalf("a truncated listing reports a total of %d, want 3", payload.CachedCount)
	}
}

func TestInspectRefusesAMalformedLimit(t *testing.T) {
	// Defaulting would mean a typo silently returns a truncated answer that looks
	// complete, which for this route is the one wrong thing to do.
	for _, query := range []string{"?limit=abc", "?limit=-1", "?limit=1.5"} {
		recorder := httptest.NewRecorder()
		server := &Server{store: listingStore(threeCachedObjects())}
		server.writeInspect(recorder, httptest.NewRequest(http.MethodGet, "/_stow/inspect"+query, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("inspect%s: status %d, want 400. A malformed limit must be refused, not defaulted.", query, recorder.Code)
		}
	}
}

func TestInspectCapsTheLimitRatherThanRefusingIt(t *testing.T) {
	// A large ask is clamped, not rejected: the caller wanted as much as it could
	// have, and refusing would make a page size an obstacle rather than a bound.
	payload := inspect(t, listingStore(threeCachedObjects()), "?limit=1000000")
	if keys := keysOf(payload); len(keys) != 3 {
		t.Fatalf("an over-large limit returned %d keys, want all 3", len(keys))
	}
}

func TestInspectListsNothingWhenTheStoreHasNoCache(t *testing.T) {
	// A store with no cache is the normal case for local mode, and the route has to
	// answer rather than fail: absence of a cache is a fact, not an error.
	payload := inspect(t, storage.NewMemoryStore(), "")
	if keys := keysOf(payload); len(keys) != 0 {
		t.Fatalf("a store with no cache listed %v", keys)
	}
	if payload.CachedCount != 0 {
		t.Fatalf("a store with no cache reports %d keys", payload.CachedCount)
	}
}
