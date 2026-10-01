package runthrough_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The propagation funnel runs outside the runtime instance — a per-second worker and
// the admin retry route — so it is the one place a reach upstream happens without
// anything having asked the policy first. Before ResourcePolicy the write path's check
// was a check of the past: it said the key was writable when the caller asked, not
// when the bytes went.

// policyFixture is a durable adapter over a file-backed outbox, so two adapters can
// be built over the same pending entries.
type policyFixture struct {
	adapter  *runthrough.Adapter
	local    storage.Store
	outbox   runthrough.Outbox
	upstream *mockUpstream
	path     string
}

func newPolicyFixture(t *testing.T, set *policy.Set, granted *authority.Authority) *policyFixture {
	t.Helper()
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	path := filepath.Join(t.TempDir(), "outbox.json")
	outbox, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	upstream := newMockUpstream()
	cfg := runthrough.Config{
		Policy:          runthrough.PolicyMirrorWrites,
		AllowLiveWrites: true,
		ResourcePolicy:  set,
		Authority:       granted,
	}
	return &policyFixture{
		adapter:  runthrough.NewWithOutbox(cfg, local, local, upstream, outbox),
		local:    local,
		outbox:   outbox,
		upstream: upstream,
		path:     path,
	}
}

// seedLocal writes an object the local store holds, which is what a propagation reads.
func (f *policyFixture) seedLocal(t *testing.T, key string) {
	t.Helper()
	if _, err := f.local.PutObject(context.Background(), "bucket", key, strings.NewReader("v"), storage.PutOptions{}); err != nil {
		t.Fatalf("seed %s: %v", key, err)
	}
}

// enqueue seeds an entry directly, which is the bug's shape: an entry exists and the
// paths that drain it are outside anything the write path checked.
func (f *policyFixture) enqueue(t *testing.T, key string) {
	t.Helper()
	if _, err := f.outbox.Enqueue(runthrough.OutboxEntry{
		Operation:      runthrough.OutboxPut,
		Bucket:         "bucket",
		Key:            key,
		UpstreamAbsent: true,
	}); err != nil {
		t.Fatalf("enqueue %s: %v", key, err)
	}
}

func (f *policyFixture) upstreamWrites(key string) int {
	f.upstream.mu.Lock()
	defer f.upstream.mu.Unlock()
	_, ok := f.upstream.objects[objectKey("bucket", key)]
	if !ok {
		return 0
	}
	return 1
}

func prefixSet(revision string, effect policy.Effect, ops ...authority.Operation) *policy.Set {
	var set policy.Set
	set.Revision = revision
	set.Add(policy.LocalNamespace, "bucket",
		policy.Selector{Kind: policy.KindObject, Prefix: "public/"}, effect, ops...)
	return &set
}

func TestTheRetryWorkerConsultsThePolicyNotJustTheEnvironment(t *testing.T) {
	// An allow over public/ and nothing else, so every key outside it is refused
	// by the policy rather than by the environment, which is all.
	allow := prefixSet("rev-1", policy.Allow, authority.UpstreamRead, authority.UpstreamWrite)
	// The deny is written over the same prefix, which is what makes the two keys
	// differ: a grant covering public/ does not cover private/, and an entry for
	// private/ must not reach the upstream on the strength of a grant elsewhere.
	allow.Add(policy.LocalNamespace, "bucket",
		policy.Selector{Kind: policy.KindObject, Exact: "private/blocked"}, policy.Deny,
		authority.UpstreamRead, authority.UpstreamWrite)
	fixture := newPolicyFixture(t, allow, nil)
	ctx := context.Background()
	for _, key := range []string{"public/ok", "private/blocked"} {
		fixture.seedLocal(t, key)
		fixture.enqueue(t, key)
	}
	if err := fixture.adapter.RetryPending(ctx); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := fixture.upstreamWrites("public/ok"); got != 1 {
		t.Errorf("the permitted key reached upstream %d times, want 1", got)
	}
	if got := fixture.upstreamWrites("private/blocked"); got != 0 {
		t.Errorf("a key the policy denies reached upstream %d times, want 0", got)
	}
}

func TestARevisedPolicyStopsAWriteThatWasAlreadyEnqueued(t *testing.T) {
	first := prefixSet("rev-1", policy.Allow, authority.UpstreamRead, authority.UpstreamWrite)
	fixture := newPolicyFixture(t, first, nil)
	ctx := context.Background()
	fixture.seedLocal(t, "public/one")
	fixture.enqueue(t, "public/one")
	if err := fixture.adapter.RetryPending(ctx); err != nil {
		t.Fatalf("first retry: %v", err)
	}
	if got := fixture.upstreamWrites("public/one"); got != 1 {
		t.Fatalf("the permitted write did not propagate, %d upstream objects", got)
	}
	// A second entry, admitted under the same grant and still pending: nothing has
	// drained it yet, and that is the state a revision change has to be caught in.
	fixture.seedLocal(t, "public/two")
	fixture.enqueue(t, "public/two")
	if got := fixture.upstreamWrites("public/two"); got != 0 {
		t.Fatalf("an undrained entry reached upstream %d times", got)
	}

	// Now the revision changes. A restart is the only way to observe a check that
	// happens at propagation rather than at enqueue: the outbox is a file, so a new
	// adapter over the same path sees the same pending entry.
	revoked := prefixSet("rev-2", policy.Deny, authority.UpstreamRead, authority.UpstreamWrite)
	restarted := newPolicyFixtureAt(t, revoked, nil, fixture)
	if err := restarted.adapter.RetryPending(ctx); err != nil {
		t.Fatalf("retry after restart: %v", err)
	}
	if got := fixture.upstreamWrites("public/two"); got != 0 {
		t.Errorf("a write admitted under rev-1 propagated under rev-2, which denies it: %d", got)
	}
	entries := restarted.adapter.OutboxEntries()
	if len(entries) != 1 {
		t.Fatalf("the outbox holds %d entries after a refused propagation, want the 1 pending", len(entries))
	}
	if entries[0].Terminal {
		t.Errorf("entry %q went terminal because the policy refused to propagate it", entries[0].ID)
	}
}

func newPolicyFixtureAt(t *testing.T, set *policy.Set, granted *authority.Authority, prior *policyFixture) *policyFixture {
	t.Helper()
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	outbox, err := runthrough.NewFileOutbox(prior.path)
	if err != nil {
		t.Fatalf("reopen outbox: %v", err)
	}
	// The first adapter's provider is reused, so "did it propagate" is one
	// question asked of one provider rather than two.
	upstream := prior.upstream
	cfg := runthrough.Config{
		Policy:          runthrough.PolicyMirrorWrites,
		AllowLiveWrites: true,
		ResourcePolicy:  set,
		Authority:       granted,
	}
	return &policyFixture{
		adapter:  runthrough.NewWithOutbox(cfg, local, local, upstream, outbox),
		local:    local,
		outbox:   outbox,
		upstream: upstream,
		path:     prior.path,
	}
}

func TestAReadThroughFetchConsultsThePolicyOnTheKeyNotTheBucket(t *testing.T) {
	allow := prefixSet("rev-1", policy.Allow, authority.UpstreamRead, authority.UpstreamWrite)
	allow.Add(policy.LocalNamespace, "bucket",
		policy.Selector{Kind: policy.KindObject, Exact: "private/blocked"}, policy.Deny,
		authority.UpstreamRead, authority.UpstreamWrite)
	fixture := newPolicyFixture(t, allow, nil)
	ctx := context.Background()
	// Only the upstream has these, so a read that is not answered as absent has to
	// have reached the provider.
	for _, key := range []string{"public/ok", "private/blocked"} {
		if _, err := fixture.upstream.PutObject(ctx, "bucket", key, strings.NewReader("v"), storage.PutOptions{}); err != nil {
			t.Fatalf("upstream put: %v", err)
		}
	}
	if _, _, err := fixture.adapter.GetObject(ctx, "bucket", "public/ok"); err != nil {
		t.Errorf("a read the policy permits was refused: %v", err)
	}
	if _, _, err := fixture.adapter.GetObject(ctx, "bucket", "private/blocked"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Errorf("a read the policy denies = %v, want the key to read as absent", err)
	}
}

// A widening resource policy refuses to reach upstream at all, rather than being
// clipped to what the environment allows.
//
// The environment is All() less EnvironmentDestroy, so it still permits UpstreamWrite.
// With a read-only environment the funnel refuses before the policy is consulted, and
// the case then passes whether the policy is refused, clipped, or never read.
func TestAWideningResourcePolicyRefusesToReachUpstreamAtAll(t *testing.T) {
	granted := authority.All().Without(authority.EnvironmentDestroy)
	widening := prefixSet("widening", policy.Allow, authority.UpstreamWrite, authority.EnvironmentDestroy)
	fixture := newPolicyFixture(t, widening, &granted)
	ctx := context.Background()
	fixture.seedLocal(t, "public/ok")
	fixture.enqueue(t, "public/ok")
	if err := fixture.adapter.RetryPending(ctx); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := fixture.upstreamWrites("public/ok"); got != 0 {
		t.Errorf("a widening policy propagated %d writes, want 0", got)
	}
	// And the same environment without the policy does propagate, so the refusal
	// above is the policy's and not the environment's.
	clean := prefixSet("clean", policy.Allow, authority.UpstreamWrite)
	allowed := newPolicyFixture(t, clean, &granted)
	allowed.seedLocal(t, "public/ok")
	allowed.enqueue(t, "public/ok")
	if err := allowed.adapter.RetryPending(ctx); err != nil {
		t.Fatalf("retry without a widening policy: %v", err)
	}
	if got := allowed.upstreamWrites("public/ok"); got != 1 {
		t.Errorf("the same environment propagated %d writes without a widening policy, want 1", got)
	}
}
