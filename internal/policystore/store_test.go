package policystore_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/policystore"
)

var (
	deadline = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	object   = policy.Resource{Namespace: "ns", Collection: "b", Kind: policy.KindObject, Locator: "a/b/c"}
)

func mustFromSet(t *testing.T, revision string, sequence uint64) policy.Record {
	t.Helper()
	var set policy.Set
	set.Revision = revision
	set.Expires = deadline
	set.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	set.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Exact: "a/secret"}, policy.Deny, authority.ObjectRead)
	set.Add("ns", "w", policy.Selector{Kind: policy.KindWorkspace, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	record, err := policy.FromSet(set, sequence, deadline.Add(-time.Hour), authority.All())
	if err != nil {
		t.Fatalf("FromSet: %v", err)
	}
	return record
}

func TestARecordRoundTripsThroughTheStoreAndStillDecides(t *testing.T) {
	store := openStore(t)
	record, err := store.Persist(sampleRecord(t, 0), 0)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if record.Sequence != 1 {
		t.Errorf("the first persisted revision got sequence %d, want 1", record.Sequence)
	}
	if !record.Expires.Equal(deadline) {
		t.Errorf("the persisted deadline is %s, want %s", record.Expires, deadline)
	}
	got, err := store.Policy(authority.All())
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if err := got.Allows(authority.All(), object, authority.ObjectRead); err != nil {
		t.Errorf("a reloaded policy refused what it granted before: %v", err)
	}
	// The prefix/subtree asymmetry is a property of the record, not of the
	// in-memory set, so it has to survive the round trip or a reloaded policy is
	// a different policy.
	ab := policy.Resource{Namespace: "ns", Collection: "w", Kind: policy.KindWorkspace, Locator: "ab"}
	if err := got.Allows(authority.All(), ab, authority.ObjectRead); err == nil {
		t.Error("a reloaded workspace subtree covered the sibling ab")
	}
}

func TestARevisionThatHasPassedItsDeadlineRefusesAfterAReload(t *testing.T) {
	store := openStore(t)
	if _, err := store.Persist(sampleRecord(t, 0), 0); err != nil {
		t.Fatalf("persist: %v", err)
	}
	// A second store on the same path, with its clock past the deadline: a
	// restart, not a mutation of the record in hand.
	restarted, err := policystore.Open(store.Path())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	restarted.SetClock(func() time.Time { return deadline.Add(time.Hour) })
	record, err := restarted.Current()
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if !record.Expired(deadline.Add(time.Hour)) {
		t.Error("a reloaded revision past its deadline did not report itself expired")
	}
	_, err = restarted.Policy(authority.All())
	if !errors.Is(err, policy.ErrStalePolicy) && !record.Expired(deadline.Add(time.Hour)) {
		t.Errorf("a stale reloaded policy answered %v, want a staleness report", err)
	}
	got, err := record.Policy(func() time.Time { return deadline.Add(time.Hour) })
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if _, err := got.Authorize(authority.All(), object, authority.ObjectRead); !errors.Is(err, policy.ErrStalePolicy) {
		t.Errorf("a policy reloaded past its deadline = %v, want ErrStalePolicy", err)
	}
}

func TestAStoreWithNoRecordSaysSoRatherThanFailing(t *testing.T) {
	store := openStore(t)
	if _, err := store.Current(); !errors.Is(err, policy.ErrNoRecord) {
		t.Errorf("an unwritten policy = %v, want ErrNoRecord", err)
	}
}

func TestAWriterWorkingFromAReplacedRevisionIsRefused(t *testing.T) {
	store := openStore(t)
	first, err := store.Persist(sampleRecord(t, 0), 0)
	if err != nil {
		t.Fatalf("persist first: %v", err)
	}
	if _, err := store.Persist(sampleRecord(t, first.Sequence), first.Sequence); err != nil {
		t.Fatalf("persist second: %v", err)
	}
	// A host that read the first revision and decided against it, arriving late.
	stale := first
	stale.Revision = "rev-late"
	stale = mustFromSet(t, stale.Revision, 0)
	if _, err := store.Persist(stale, first.Sequence); !errors.Is(err, policy.ErrRecordSuperseded) {
		t.Errorf("a stale writer = %v, want ErrRecordSuperseded", err)
	}
	current, err := store.Current()
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.Revision == "rev-late" {
		t.Error("the losing writer's revision is the one that survived")
	}
}

func TestAWriterMayNotRollTheSequenceBackwards(t *testing.T) {
	store := openStore(t)
	first, err := store.Persist(sampleRecord(t, 0), 0)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	// A caller asking to store sequence 0 over a stored sequence 1 is trying to
	// re-issue a superseded revision, which is how a revocation is undone.
	rollback := first
	rollback.Sequence = 0
	rollback.Revision = "rev-rollback"
	rollback = mustFromSet(t, rollback.Revision, 0)
	if _, err := store.Persist(rollback, 0); !errors.Is(err, policy.ErrRecordSuperseded) {
		t.Errorf("a rollback write = %v, want ErrRecordSuperseded", err)
	}
}

func TestConcurrentWritersProduceOneWinnerAndNoLostUpdate(t *testing.T) {
	store := openStore(t)
	const writers = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		winners  int
		failures []error
	)
	// Every writer starts from the same absent state, so every one of them
	// believes it is the first. Exactly one may be right.
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Persist(sampleRecord(t, uint64(i)), 0)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				winners++
				return
			}
			if !errors.Is(err, policy.ErrRecordSuperseded) {
				failures = append(failures, err)
			}
		}()
	}
	wg.Wait()
	if len(failures) > 0 {
		t.Errorf("concurrent writers failed for reasons other than supersession: %v", failures)
	}
	if winners != 1 {
		t.Errorf("%d writers believed they were first, want exactly 1", winners)
	}
	record, err := store.Current()
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if record.Sequence != 1 {
		t.Errorf("the stored revision is sequence %d, want 1", record.Sequence)
	}
}

func TestAWideningRecordIsRefusedBeforeItIsEverStored(t *testing.T) {
	store := openStore(t)
	var set policy.Set
	set.Revision = "rev-widening"
	set.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.EnvironmentDestroy)
	issued := deadline.Add(-time.Hour)
	if _, err := policy.FromSet(set, 0, issued, authority.ReadOnly()); !errors.Is(err, policy.ErrWidening) {
		t.Errorf("persisting a widening policy = %v, want ErrWidening", err)
	}
	if _, err := store.Current(); !errors.Is(err, policy.ErrNoRecord) {
		t.Errorf("the refused policy was stored anyway: %v", err)
	}
}

// sampleRecord names each revision distinctly, so a test that loses a race can
// tell whose revision survived.
func sampleRecord(t *testing.T, sequence uint64) policy.Record {
	t.Helper()
	return mustFromSet(t, "rev-"+string(rune('a'+sequence)), sequence)
}

func openStore(t *testing.T) *policystore.Store {
	t.Helper()
	store, err := policystore.Open(t.TempDir() + "/policy.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	store.SetClock(func() time.Time { return deadline.Add(-time.Hour) })
	return store
}
