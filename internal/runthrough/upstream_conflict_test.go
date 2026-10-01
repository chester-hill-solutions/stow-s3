package runthrough_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// A second writer's update must not be silently overwritten by propagation.
//
// This is the defect. propagateWrite checked only that the *local* version still matched
// its outbox entry, and used the upstream ETag comparison purely as a crash-dedup test:
// if the ETags differed it concluded the write had not landed and overwrote upstream.
// So a device write followed by an agent write lost the device's object.
//
// The fix is a precondition rather than a comparison: record what upstream held when
// the write was enqueued and require it to still hold. Where upstream held nothing,
// that requirement is If-None-Match "*", which catches the concurrent-create race.

// conflictUpstream is an upstream that a second writer has already moved on from.
func conflictUpstream(t *testing.T, ctx context.Context, body string) *mockUpstream {
	t.Helper()
	up := newMockUpstream()
	if _, err := up.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte(body)), storage.PutOptions{}); err != nil {
		t.Fatalf("second writer upstream put: %v", err)
	}
	return up
}

func mirroringAdapter(t *testing.T, local storage.Store, up *mockUpstream) *runthrough.Adapter {
	t.Helper()
	outbox, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	cache := storage.NewMemoryStore()
	if err := cache.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatalf("create cache bucket: %v", err)
	}
	// A separate cache, as production uses. The cache is where stow records which
	// upstream state a local copy was derived from, so a test sharing one store
	// for both would not exercise the path that detects a conflict.
	return runthrough.NewWithOutbox(runthrough.Config{
		Policy:          runthrough.PolicyMirrorWrites,
		AllowLiveWrites: true,
		Upstream:        runthrough.UpstreamConfig{Bucket: "bucket"},
	}, local, cache, up, outbox)
}

// readThrough makes stow fetch the key from upstream, which is what records the
// provenance a later write is checked against. This is the agent flow: read the
// file, then edit it.
func readThrough(t *testing.T, adapter *runthrough.Adapter, ctx context.Context) {
	t.Helper()
	rc, _, err := adapter.GetObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatalf("read through: %v", err)
	}
	_ = rc.Close()
}

func TestPropagationDoesNotOverwriteAConcurrentlyChangedObject(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	// The agent reads the file, so stow records which upstream state its copy came
	// from. Then the device writes, and only then does the agent write.
	up := conflictUpstream(t, ctx, "original from upstream")

	adapter := mirroringAdapter(t, local, up)
	readThrough(t, adapter, ctx)

	// The other writer moves on after stow read but before stow writes. This is
	// the case a fresh HeadObject at enqueue time cannot see, because it would
	// report this new state and match it.
	if _, err := up.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("written by the device")), storage.PutOptions{}); err != nil {
		t.Fatalf("second writer: %v", err)
	}

	// The conflict surfaces from the write itself, not from a later retry.
	// completeIntentWithScheduleLocked propagates inline, so a caller learns
	// immediately rather than after a retry cycle. That is the pre-existing
	// contract for any propagation failure and the conflict now follows it, which
	// is what makes the conflict actionable: an agent finds out before it builds
	// more work on a divergent state.
	if _, err := adapter.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("written by the agent")), storage.PutOptions{}); !errors.Is(err, runthrough.ErrUpstreamConflict) {
		t.Fatalf("agent put error = %v, want ErrUpstreamConflict", err)
	}

	// The other writer's bytes must survive. This is the assertion that would
	// have failed before the fix, and it is the one that matters: a conflict that
	// is reported but still destroys the object is not a conflict.
	rc, meta, err := up.GetObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatalf("read upstream after conflict: %v", err)
	}
	defer rc.Close()
	got := make([]byte, meta.Size)
	if _, err := readFull(rc, got); err != nil {
		t.Fatalf("read upstream body: %v", err)
	}
	if string(got) != "written by the device" {
		t.Fatalf("upstream body = %q, want the other writer's bytes to survive", got)
	}
}

func TestAConflictIsTerminalAndNotRetriedForever(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	up := conflictUpstream(t, ctx, "original from upstream")

	adapter := mirroringAdapter(t, local, up)
	readThrough(t, adapter, ctx)
	if _, err := up.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("written by the device")), storage.PutOptions{}); err != nil {
		t.Fatalf("second writer: %v", err)
	}

	if _, err := adapter.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("written by the agent")), storage.PutOptions{}); !errors.Is(err, runthrough.ErrUpstreamConflict) {
		t.Fatalf("agent put error = %v, want ErrUpstreamConflict", err)
	}

	pending, terminal := adapter.OutboxStats()
	if pending != 0 || terminal != 1 {
		t.Fatalf("outbox stats = %d pending / %d terminal, want 0 pending and 1 terminal", pending, terminal)
	}

	// Retrying must not resurrect it. A precondition failure cannot succeed
	// without a new decision, so an entry left retryable would spin forever against
	// an upstream that has not changed and will not. A terminal entry is skipped
	// rather than re-reported, so the retry reports no work and the upstream object
	// is still the other writer's — which is the property, rather than the shape of
	// the return value.
	if err := adapter.RetryPending(ctx); err != nil {
		t.Fatalf("retry after a terminal conflict: %v", err)
	}
	pending, terminal = adapter.OutboxStats()
	if pending != 0 || terminal != 1 {
		t.Fatalf("outbox stats after retry = %d pending / %d terminal, want 0 / 1", pending, terminal)
	}
	rc, meta, err := up.GetObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatalf("read upstream after retry: %v", err)
	}
	defer rc.Close()
	got := make([]byte, meta.Size)
	if _, err := readFull(rc, got); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != "written by the device" {
		t.Fatalf("upstream body = %q, want the retry not to have overwritten it", got)
	}
}

func TestAFreshKeyPropagatesWhenUpstreamStillLacksIt(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	up := newMockUpstream()

	adapter := mirroringAdapter(t, local, up)
	if _, err := adapter.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("first")), storage.PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := adapter.RetryPending(ctx); err != nil {
		t.Fatalf("propagation of an uncontested new key: %v", err)
	}
	rc, meta, err := up.GetObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	defer rc.Close()
	got := make([]byte, meta.Size)
	if _, err := readFull(rc, got); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("upstream body = %q, want %q", got, "first")
	}
}

// The boundary: a key stow has never read has no provenance to defend, so the
// write propagates and last-writer-wins continues to apply.
//
// This is stated rather than left implicit, because it is the honest limit of
// what conflict detection can claim. stow can only detect that upstream moved
// away from a state *it* observed. For a key it created without reading
// anything there is no such state, and refusing would break the ordinary case
// of an agent writing a new file. Asserted so the boundary cannot widen by
// accident: a change that made every write conflict would fail here.
//
// A concurrent create is not tested here because it is unreachable through this
// API rather than merely unproven: propagation is inline, so a write has already
// landed before anything could create the key. The reachability is the finding.
func TestAKeyWithNoProvenancePropagatesWithoutAPrecondition(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	up := newMockUpstream()

	// Upstream already holds this key, but stow has never read it, so it holds no
	// record of the state it would be defending.
	if _, err := up.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("someone else's file")), storage.PutOptions{}); err != nil {
		t.Fatalf("seed upstream: %v", err)
	}

	adapter := mirroringAdapter(t, local, up)
	if _, err := adapter.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("a brand new file")), storage.PutOptions{}); err != nil {
		t.Fatalf("write to an unobserved key was refused: %v", err)
	}
	rc, meta, err := up.GetObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	defer rc.Close()
	got := make([]byte, meta.Size)
	if _, err := readFull(rc, got); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != "a brand new file" {
		t.Fatalf("upstream body = %q, want the new file to propagate", got)
	}
}

// An uncontested update to an existing upstream object must still propagate. This
// is the regression guard: a fix that made every propagation conflict would pass
// the tests above and break the product.
func TestAnUncontestedUpdateStillPropagates(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	up := conflictUpstream(t, ctx, "original")

	// Read first, so the write is checked against real provenance rather than
	// against no record at all. Otherwise this would pass whether or not the
	// mechanism works.
	adapter := mirroringAdapter(t, local, up)
	readThrough(t, adapter, ctx)
	if _, err := adapter.PutObject(ctx, "bucket", "key", bytes.NewReader([]byte("updated")), storage.PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := adapter.RetryPending(ctx); err != nil {
		t.Fatalf("uncontested update failed to propagate: %v", err)
	}
	rc, meta, err := up.GetObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	defer rc.Close()
	got := make([]byte, meta.Size)
	if _, err := readFull(rc, got); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != "updated" {
		t.Fatalf("upstream body = %q, want %q", got, "updated")
	}
}

func readFull(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if total == len(buf) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}
