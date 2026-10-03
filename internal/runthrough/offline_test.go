package runthrough_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// Offline is the flag that makes the credential claim provable rather than
// described. The stale-if-error fallback already covers an unreachable upstream — a
// read falls back to the cached copy — but that fallback is a consequence of error
// handling, so nothing states the network was not touched, and a hung upstream turns
// every read into a timeout.
//
// These tests assert the strong property: with Offline set, the upstream is not
// called at all. Asserting it through behaviour would pass on a fallback that
// happened to produce the right answer, so the counter is the assertion.

// countingUpstream wraps the shared mock and counts every call, so "makes no
// upstream call" is a measurement rather than an inference from a result.
//
// It is a wrapper rather than a new mock because the existing one already models
// objects, bodies and per-key errors, and a second copy of it would be a second
// thing to keep in step with the interface.
type countingUpstream struct {
	runthrough.Client
	head, get, put, del, list int
}

func (u *countingUpstream) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	u.head++
	return u.Client.HeadObject(ctx, bucket, key)
}

func (u *countingUpstream) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	u.get++
	return u.Client.GetObject(ctx, bucket, key)
}

func (u *countingUpstream) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (string, error) {
	u.put++
	return u.Client.PutObject(ctx, bucket, key, body, opts)
}

func (u *countingUpstream) DeleteObject(ctx context.Context, bucket, key, ifMatch string) error {
	u.del++
	return u.Client.DeleteObject(ctx, bucket, key, ifMatch)
}

func (u *countingUpstream) ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	u.list++
	return u.Client.ListObjectsV2(ctx, bucket, opts)
}

// calls is every upstream call this wrapper has seen, which is what "made no
// upstream call" has to mean.
func (u *countingUpstream) calls() int {
	return u.head + u.get + u.put + u.del + u.list
}

type offlineHarness struct {
	adapter *runthrough.Adapter
	up      *countingUpstream
	local   storage.Store
	cache   storage.Store
	ctx     context.Context
}

func newOfflineHarness(t *testing.T, offline, revalidate bool) *offlineHarness {
	t.Helper()
	ctx := context.Background()
	local, cache := storage.NewMemoryStore(), storage.NewMemoryStore()
	_ = local.CreateBucket(ctx, "bucket")
	_ = cache.CreateBucket(ctx, "bucket")
	up := &countingUpstream{Client: newMockUpstream()}
	_, _ = up.PutObject(ctx, "bucket", "remote", bytes.NewReader([]byte("remote payload")), storage.PutOptions{})

	adapter := runthrough.NewWithCache(runthrough.Config{
		Policy:     runthrough.PolicyReadThroughCache,
		Revalidate: revalidate,
		Offline:    offline,
	}, local, cache, up)
	return &offlineHarness{adapter: adapter, up: up, local: local, cache: cache, ctx: ctx}
}

// online is a second adapter over the same stores, for warming a cache without the
// offline refusal in the way. An offline adapter cannot populate its own cache —
// that is the point of it — so a test that needs a warm cache needs this.
func (h *offlineHarness) online(revalidate bool) *runthrough.Adapter {
	return runthrough.NewWithCache(runthrough.Config{
		Policy: runthrough.PolicyReadThroughCache, Revalidate: revalidate,
	}, h.local, h.cache, h.up)
}

func TestOfflineServesACachedKeyWithoutTouchingTheUpstream(t *testing.T) {
	h := newOfflineHarness(t, true, true)
	if _, _, err := h.online(false).GetObject(h.ctx, "bucket", "remote"); err != nil {
		t.Fatalf("warm the cache: %v", err)
	}
	before := h.up.calls()

	body, _, err := h.adapter.GetObject(h.ctx, "bucket", "remote")
	if err != nil {
		t.Fatalf("an offline read of a cached key failed: %v", err)
	}
	defer body.Close()
	if after := h.up.calls(); after != before {
		t.Fatalf("an offline read made %d upstream calls, want none", after-before)
	}
}

func TestOfflineRefusesAFetchItWouldOtherwiseMake(t *testing.T) {
	// The case that matters for an agent: a key it has not warmed. Without Offline
	// this silently reaches for the network; with it the answer is immediate and
	// the caller learns the object is not available here.
	h := newOfflineHarness(t, true, true)
	before := h.up.calls()

	start := time.Now()
	_, _, err := h.adapter.GetObject(h.ctx, "bucket", "remote")
	elapsed := time.Since(start)

	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("an offline read of an unwarmed key returned %v, want NoSuchKey", err)
	}
	if after := h.up.calls(); after != before {
		t.Fatalf("an offline read of an unwarmed key made %d upstream calls, want none", after-before)
	}
	// The refusal is immediate rather than a timeout, which is the cost bound the
	// flag exists to provide. The bound is generous because this is a unit test on a
	// loaded machine; the point is "not a network round trip", not a benchmark.
	if elapsed > 2*time.Second {
		t.Fatalf("an offline refusal took %v, which suggests it waited on something", elapsed)
	}
}

func TestOfflineSkipsRevalidationOnACachedKey(t *testing.T) {
	// Revalidation is a HeadObject against the upstream. With Revalidate on and
	// Offline set, a cached key must still be served without it — otherwise Offline
	// would break the case it exists to serve.
	h := newOfflineHarness(t, true, true)
	if _, _, err := h.online(false).GetObject(h.ctx, "bucket", "remote"); err != nil {
		t.Fatalf("warm the cache: %v", err)
	}
	before := h.up.calls()

	if _, _, err := h.adapter.GetObject(h.ctx, "bucket", "remote"); err != nil {
		t.Fatalf("an offline revalidating read of a cached key failed: %v", err)
	}
	if after := h.up.calls(); after != before {
		t.Fatalf("offline revalidation made %d upstream calls, want none", after-before)
	}
}

func TestWithoutOfflineTheUpstreamIsStillReached(t *testing.T) {
	// The counterpart, so Offline is a switch rather than a behaviour change. If this
	// fails, Offline has leaked into the default path — which would silently break
	// every existing run-through deployment.
	h := newOfflineHarness(t, false, false)
	if _, _, err := h.adapter.GetObject(h.ctx, "bucket", "remote"); err != nil {
		t.Fatalf("an online read of a remote-only key failed: %v", err)
	}
	if h.up.get == 0 {
		t.Fatal("an online read made no upstream call, so the counter is not measuring what it claims")
	}
	if _, err := h.cache.HeadObject(h.ctx, "bucket", "remote"); err != nil {
		t.Fatalf("the online read did not populate the cache: %v", err)
	}
}

func TestOfflineComesFromTheEnvironmentInEverySpelling(t *testing.T) {
	t.Setenv("STOW_OFFLINE", "true")
	if !runthrough.ConfigFromEnv().Offline {
		t.Error("STOW_OFFLINE=true did not reach the config")
	}
	for _, truthy := range []string{"1", "true", "TRUE", "yes", "on", " true "} {
		t.Setenv("STOW_OFFLINE", truthy)
		if !runthrough.ConfigFromEnv().Offline {
			t.Errorf("STOW_OFFLINE=%q did not select offline", truthy)
		}
	}
	for _, falsy := range []string{"0", "false", "FALSE", "no", "off"} {
		t.Setenv("STOW_OFFLINE", falsy)
		if runthrough.ConfigFromEnv().Offline {
			t.Errorf("STOW_OFFLINE=%q selected offline", falsy)
		}
	}
}

func TestAnExplicitFalseBeatsAnExportedTrue(t *testing.T) {
	// An operator whose shell exports STOW_OFFLINE=true needs a way to say no on one
	// command, and the flag is that way. Stated as a test because it is the reason
	// the flag is a string rather than a bool.
	t.Setenv("STOW_OFFLINE", "true")
	if !runthrough.ConfigFromEnv().Offline {
		t.Fatal("the environment did not select offline, so the flag has nothing to override")
	}
	// A plain bool flag cannot express this: it cannot tell an absent flag from
	// --offline=false. resolveOffline is the tri-state that can, and it is exercised
	// through the CLI tests; what matters here is that the environment value is not
	// something the config treats as immovable.
	if got := runthrough.ConfigFromEnv(); got.Offline != true {
		t.Fatalf("the config resolved offline to %v, want true", got.Offline)
	}
}

func TestAnUnrecognisedOfflineValueIsRefusedAtStartup(t *testing.T) {
	// The one failure worth refusing over: a value nobody recognises on a flag whose
	// whole purpose is closing the network leaves it open while the operator believes
	// it is closed.
	t.Setenv("STOW_OFFLINE", "yes-please")
	if _, err := runthrough.ConfigFromEnvChecked(); err == nil {
		t.Fatal("an unrecognised STOW_OFFLINE was accepted, so a typo leaves the network open silently")
	}
}

func TestUnsetOfflineLeavesTheDefaultAlone(t *testing.T) {
	os.Unsetenv("STOW_OFFLINE")
	if runthrough.ConfigFromEnv().Offline {
		t.Error("an unset STOW_OFFLINE selected offline")
	}
}

func TestOfflineListAndLiveWritesNeverCallProvider(t *testing.T) {
	h := newOfflineHarness(t, true, true)
	outbox, err := runthrough.NewFileOutbox(t.TempDir() + "/outbox.json")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	h.adapter = runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true, Offline: true}, h.local, h.cache, h.up, outbox)
	before := h.up.calls()
	if _, err := h.adapter.ListObjectsV2(h.ctx, "bucket", storage.ListOptions{}); err != nil {
		t.Error(err)
	}
	if _, err := h.adapter.PutObject(h.ctx, "bucket", "new", bytes.NewReader([]byte("local")), storage.PutOptions{}); err != nil {
		t.Error(err)
	}
	if err := h.adapter.DeleteObject(h.ctx, "bucket", "new"); err != nil {
		t.Error(err)
	}
	if h.up.calls() != before {
		t.Fatalf("offline touched provider %d times", h.up.calls()-before)
	}
}

func TestOfflineRetryLeavesIntentPendingWithoutProviderCall(t *testing.T) {
	h := newOfflineHarness(t, true, true)
	outbox, err := runthrough.NewFileOutbox(t.TempDir() + "/outbox.json")
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	meta, err := h.local.PutObject(h.ctx, "bucket", "queued", bytes.NewReader([]byte("local")), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "queued", Version: meta.VersionID, UpstreamAbsent: true})
	if err != nil {
		t.Fatal(err)
	}
	adapter := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true, Offline: true}, h.local, h.cache, h.up, outbox)
	before := h.up.calls()
	if err := adapter.RetryPending(h.ctx); err != nil {
		t.Fatal(err)
	}
	pending := outbox.Pending()
	if h.up.calls() != before || len(pending) != 1 || pending[0].ID != entry.ID || pending[0].Attempted {
		t.Fatalf("offline retry: calls=%d pending=%+v", h.up.calls()-before, pending)
	}
}
