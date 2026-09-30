package runthrough_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// R-201 and ADR 0010 decision 2: no code path may reach an upstream provider
// without the environment's grant permitting it, and the grant - not a
// configuration flag - is what answers.
//
// The counts are on the provider, not on the error. A refusal that looks like a
// successful read is the failure mode, so every case asserts what did not happen.

// adapterForDurable is adapterFor with a durable, coordinated outbox.
//
// Propagation requires one: requireDurableOutbox refuses a mirror-writes
// propagation through a MemoryOutbox with ErrDurableOutboxRequired, before any
// grant is consulted. Testing the grant through one therefore tests the outbox.
func adapterForDurable(t *testing.T, cfg runthrough.Config, granted *authority.Authority) (*runthrough.Adapter, *mockUpstream) {
	t.Helper()
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	cfg.Policy = runthrough.PolicyMirrorWrites
	cfg.AllowLiveWrites = true
	if granted != nil {
		cfg.Authority = granted
	}
	outbox, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	upstream := newMockUpstream()
	return runthrough.NewWithOutbox(cfg, local, local, upstream, outbox), upstream
}

// ptr is the address of a value, for the Config field that takes a pointer.
func ptr(a authority.Authority) *authority.Authority { return &a }

func grantWithoutUpstream() authority.Authority {
	return authority.All().Without(authority.UpstreamRead, authority.UpstreamWrite)
}

// adapterFor builds a run-through adapter over a local store that already holds
// one object, so a read of anything else is a miss that would consult upstream.
func adapterFor(t *testing.T, cfg runthrough.Config, granted *authority.Authority) (*runthrough.Adapter, *mockUpstream) {
	t.Helper()
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := local.PutObject(ctx, "bucket", "local-only", bytes.NewReader([]byte("value")), storage.PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	cfg.Policy = runthrough.PolicyMirrorWrites
	cfg.AllowLiveWrites = true
	if granted != nil {
		cfg.Authority = granted
	}
	upstream := newMockUpstream()
	return runthrough.NewWithOutbox(cfg, local, local, upstream, runthrough.NewMemoryOutbox()), upstream
}

func upstreamCalls(m *mockUpstream) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.headCalls + m.getCalls + m.putCalls + m.delCalls
}

// A read-through miss must not become an upstream read when the grant withholds
// UpstreamRead. The local store is authoritative either way, so the caller sees
// the same answer - which is exactly why this has to be asserted on the provider.
func TestReadThroughDoesNotReachUpstreamWithoutTheGrant(t *testing.T) {
	granted := grantWithoutUpstream()
	adapter, upstream := adapterFor(t, runthrough.Config{}, &granted)

	if _, _, err := adapter.GetObject(context.Background(), "bucket", "not-local"); err == nil {
		t.Fatal("GetObject of a key that is nowhere succeeded")
	}
	if calls := upstreamCalls(upstream); calls != 0 {
		t.Errorf("the grant withholds UpstreamRead but the adapter made %d upstream calls", calls)
	}
}

// A write must not be propagated when the grant withholds UpstreamWrite, and the
// local write must still succeed: refusing to reach someone else's data is not a
// reason to refuse the caller's own.
func TestWriteIsNotPropagatedWithoutTheGrant(t *testing.T) {
	granted := grantWithoutUpstream()
	adapter, upstream := adapterFor(t, runthrough.Config{}, &granted)

	if _, err := adapter.PutObject(context.Background(), "bucket", "written", bytes.NewReader([]byte("v")), storage.PutOptions{}); err != nil {
		t.Fatalf("PutObject = %v, want the local write to succeed", err)
	}
	if calls := upstreamCalls(upstream); calls != 0 {
		t.Errorf("the grant withholds UpstreamWrite but the adapter made %d upstream calls", calls)
	}
}

// Withholding only UpstreamWrite must leave reads alone. Refusing consent to
// change someone else's data has never been a reason to stop reading from them,
// and a read-through cache over a locally authoritative write depends on it.
func TestWithholdingUpstreamWriteLeavesReadsAlone(t *testing.T) {
	granted := authority.All().Without(authority.UpstreamWrite)
	adapter, upstream := adapterFor(t, runthrough.Config{}, &granted)

	if _, _, err := adapter.GetObject(context.Background(), "bucket", "not-local"); err == nil {
		t.Fatal("GetObject of a key that is nowhere succeeded")
	}
	if calls := upstreamCalls(upstream); calls == 0 {
		t.Error("withholding UpstreamWrite also stopped reads; consent to change data is not consent to read it")
	}
}

// The full grant propagates and reads through. Without this, every case above
// would pass on an adapter that never reaches upstream at all.
//
// This uses a durable outbox, and that is not incidental: MemoryOutbox reports
// Durable() == false, so a mirror-writes propagation is refused with
// ErrDurableOutboxRequired before any grant is consulted. An earlier version of
// this case used the memory outbox and passed for that reason rather than the one
// it claimed.
func TestTheFullGrantStillPropagatesAndReadsThrough(t *testing.T) {
	granted := authority.All()
	adapter, upstream := adapterForDurable(t, runthrough.Config{}, &granted)

	if _, err := adapter.PutObject(context.Background(), "bucket", "written", bytes.NewReader([]byte("v")), storage.PutOptions{}); err != nil {
		t.Fatalf("PutObject = %v", err)
	}
	if err := adapter.RetryPending(context.Background()); err != nil {
		t.Fatalf("RetryPending = %v", err)
	}
	_, _, _ = adapter.GetObject(context.Background(), "bucket", "not-local")
	if calls := upstreamCalls(upstream); calls == 0 {
		t.Error("the full grant reached nothing; the refusals above would pass on an adapter that never calls upstream")
	}
}

// AllowLiveWrites false must still mean no propagation. It passes because the
// attenuation is applied when the authority is built, not because the flag is read
// at the decision - and this case is here so that stays true, since removing the
// read is what made the earlier version of this work unsafe.
func TestAllowLiveWritesFalseStillWithholdsPropagation(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	upstream := newMockUpstream()
	cfg := runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: false}
	adapter := runthrough.NewWithOutbox(cfg, local, local, upstream, runthrough.NewMemoryOutbox())

	if _, err := adapter.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("v")), storage.PutOptions{}); err == nil {
		t.Fatal("mirror-writes without live-write consent succeeded; it must be refused")
	}
	if calls := upstreamCalls(upstream); calls != 0 {
		t.Errorf("propagated %d calls with live writes disabled", calls)
	}
}

// R-201's specific finding: RetryPending bypassed even decideUpstreamWrite, and
// runs from the per-second worker and the admin retry route, both outside the
// runtime instance. The grant is checked in the shared propagation funnel now, so
// draining the outbox is not a way around it.
//
// The entry is seeded into the outbox directly rather than produced by a write.
// That is the shape of the original bug - an entry exists, and the paths that
// drain it are outside anything that checked - and seeding it is the only way to
// reach it now that the grant is resolved once at construction, so the enqueue and
// the retry can no longer disagree.
func TestRetryPendingCannotBypassTheGrant(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	// The local object has to exist: a propagation reads the body it is sending, so
	// an entry naming a key the store does not hold fails with "object not found"
	// before it ever reaches a grant.
	if _, err := local.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("v")), storage.PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	// A durable outbox, because a MemoryOutbox refuses a mirror-writes propagation
	// with ErrDurableOutboxRequired before any grant is consulted. Three separate
	// versions of this case passed for that reason.
	outbox, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	if _, err := outbox.Enqueue(runthrough.OutboxEntry{UpstreamAbsent: true,
		Operation: runthrough.OutboxPut,
		Bucket:    "bucket",
		Key:       "key",
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	upstream := newMockUpstream()
	// Only UpstreamWrite is withheld. Withholding the read grant as well stops the
	// retry at upstreamEnabled before it reaches the propagation funnel, so the case
	// passes with the funnel's own check removed - which is what happened the first
	// two times.
	granted := authority.All().Without(authority.UpstreamWrite)
	cfg := runthrough.Config{
		Policy:          runthrough.PolicyMirrorWrites,
		AllowLiveWrites: true,
		Authority:       &granted,
	}
	adapter := runthrough.NewWithOutbox(cfg, local, local, upstream, outbox)

	before := upstreamCalls(upstream)
	if err := adapter.RetryPending(ctx); err != nil {
		t.Fatalf("RetryPending = %v", err)
	}
	if after := upstreamCalls(upstream); after != before {
		t.Errorf("RetryPending reached upstream %d times with UpstreamWrite withheld", after-before)
	}
	// And the entry survives: a refusal is not a failure, and discarding it would
	// lose a write the operator may yet authorise.
	if len(outbox.Pending()) == 0 {
		t.Error("the refused entry was discarded; it should still be pending in case the grant is given")
	}
}

// A case that was here and is not: "a refused propagation is left pending".
//
// It could not hold. Under read-through-cache with no write consent
// decideUpstreamWrite returns writeSkip, so nothing is ever enqueued and there is
// nothing to discard - the assertion could only fail, and fixing it would have
// meant asserting a different thing under the same name. The property it was after
// - that an entry the funnel refuses survives - is asserted in
// TestRetryPendingCannotBypassTheGrant, where the entry is seeded directly.

// The same seeded entry, with the full grant, does propagate.
//
// Without this the bypass case above proves nothing: an adapter that never drains
// its outbox would satisfy "no upstream calls with the grant withholding it" just
// as well as a correctly gated one. This is the control that makes the refusal
// mean something.
func TestRetryPendingPropagatesWithTheFullGrant(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := local.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("v")), storage.PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	outbox, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	if _, err := outbox.Enqueue(runthrough.OutboxEntry{UpstreamAbsent: true,
		Operation: runthrough.OutboxPut,
		Bucket:    "bucket",
		Key:       "key",
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	upstream := newMockUpstream()
	granted := authority.All()
	cfg := runthrough.Config{
		Policy:          runthrough.PolicyMirrorWrites,
		AllowLiveWrites: true,
		Authority:       &granted,
	}
	adapter := runthrough.NewWithOutbox(cfg, local, local, upstream, outbox)

	before := upstreamCalls(upstream)
	if err := adapter.RetryPending(ctx); err != nil {
		t.Fatalf("RetryPending = %v", err)
	}
	if upstreamCalls(upstream) == before {
		t.Error("the full grant propagated nothing on retry; the refusal case passes on an adapter that never drains its outbox")
	}
}
