package runtime_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// A policy read once at open could only change by restarting the process, so a
// revocation was effective when somebody restarted rather than when it was issued.
// The only way to observe the difference is two operations on one running Instance
// with the policy different between them, which is what every case here does.

// errUnknownPolicy is a source that cannot say. It is named because the assertion
// is that the answer is the source's own, which means nothing otherwise.

// rotating is a policy source whose answer the test changes between operations.
type rotating struct {
	mu    sync.Mutex
	now   policy.Set
	err   error
	calls int
}

func (r *rotating) set(next policy.Set) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = next
	r.err = nil
}

func (r *rotating) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *rotating) reads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *rotating) source() policy.Source {
	return func() (policy.Set, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.calls++
		if r.err != nil {
			return policy.Set{}, r.err
		}
		return r.now, nil
	}
}

func reading(prefix string) policy.Set {
	var set policy.Set
	set.Revision = "rev-read"
	set.Expires = time.Now().Add(time.Hour)
	set.Add(policy.LocalNamespace, "bucket",
		policy.Selector{Kind: policy.KindObject, Prefix: prefix}, policy.Allow,
		authority.ObjectRead, authority.ObjectWrite, authority.ObjectList)
	return set
}

func TestARevocationTakesEffectWithoutARestart(t *testing.T) {
	rot := &rotating{now: reading("public/")}
	store := storage.NewMemoryStore()
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend: runtime.BackendMemory,
		Policy:  rot.source(),
	}, store, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer instance.Close()
	if _, err := instance.PutObject(ctx, "bucket", "public/a", nil, runtime.PutOptions{}); err != nil {
		t.Fatalf("the permitted write was refused: %v", err)
	}
	// The policy is narrowed to a prefix that covers nothing this caller writes.
	rot.set(reading("other/"))
	if _, err := instance.PutObject(ctx, "bucket", "public/b", nil, runtime.PutOptions{}); err == nil {
		t.Fatal("a write the revised policy does not cover was permitted")
	}
	if _, err := instance.GetObject(ctx, "bucket", "public/a"); err == nil {
		t.Error("a read the revised policy does not cover was permitted")
	}
	// And a later relaxation takes effect too, so this is a live answer and not a
	// latch that only ever closes.
	rot.set(reading("public/"))
	if _, err := instance.PutObject(ctx, "bucket", "public/c", nil, runtime.PutOptions{}); err != nil {
		t.Errorf("a write the re-relaxed policy covers was refused: %v", err)
	}
}

func TestTheSourceIsConsultedPerDecisionAndNotOnceAtOpen(t *testing.T) {
	rot := &rotating{now: reading("public/")}
	instance, closeIt := openWithSource(t, rot)
	defer closeIt()
	ctx := context.Background()
	for range 3 {
		if _, err := instance.ListObjects(ctx, "bucket", runtime.ListOptions{Prefix: "public/"}); err != nil {
			t.Fatalf("list: %v", err)
		}
	}
	if got := rot.reads(); got < 3 {
		t.Errorf("three operations consulted the source %d times, so the policy is still read once at open", got)
	}
}

func TestAnUnknownPolicyIsReportedAndNotResolvedIntoADecision(t *testing.T) {
	rot := &rotating{now: reading("public/")}
	instance, closeIt := openWithSource(t, rot)
	defer closeIt()
	ctx := context.Background()
	if _, err := instance.ListObjects(ctx, "bucket", runtime.ListOptions{Prefix: "public/"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	// The answer must be the source's own, not a refusal that reads as a decision:
	// reporting "you may not" for "I do not know" is how an outage becomes a
	// permission that looks deliberate.
	rot.fail(errUnknownPolicy)
	_, err := instance.ListObjects(ctx, "bucket", runtime.ListOptions{Prefix: "public/"})
	if err == nil {
		t.Fatal("an operation proceeded on a policy whose answer is unknown")
	}
	if errors.Is(err, policy.ErrStalePolicy) {
		t.Error("an unreadable policy was reported as a stale one; the caller cannot tell them apart")
	}
	if !errors.Is(err, errUnknownPolicy) {
		t.Errorf("an unreadable policy = %v, want the source's own error", err)
	}
	// And it recovers: an unknown answer is not a latch.
	rot.set(reading("public/"))
	if _, err := instance.ListObjects(ctx, "bucket", runtime.ListOptions{Prefix: "public/"}); err != nil {
		t.Errorf("the source recovered and the operation still failed: %v", err)
	}
}

var errUnknownPolicy = errors.New("policy record could not be read")

func TestAWidtheningRevisionIsRefusedWhenItIsReadNotWhenItIsWritten(t *testing.T) {
	rot := &rotating{now: reading("public/")}
	readOnly := authority.ReadOnly()
	store := storage.NewMemoryStore()
	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend:   runtime.BackendMemory,
		Authority: &readOnly,
		Policy:    rot.source(),
	}, store, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer instance.Close()
	// A later revision can be wider than the one before it.
	var widened policy.Set
	widened.Revision = "rev-widened"
	widened.Add(policy.LocalNamespace, "bucket",
		policy.Selector{Kind: policy.KindObject, Prefix: "public/"}, policy.Allow,
		authority.ObjectWrite)
	rot.set(widened)
	_, err = instance.ListObjects(context.Background(), "bucket", runtime.ListOptions{Prefix: "public/"})
	if !errors.Is(err, policy.ErrWidening) {
		t.Errorf("a widening revision read under a narrower environment = %v, want ErrWidening", err)
	}
}

func TestAFixedSourceIgnoresATypedNilPolicy(t *testing.T) {
	// A nil *Set reaching Fixed is a caller's mistake, and the answer is the empty
	// set — which denies everything — rather than no policy, which would permit
	// everything the environment does.
	store := storage.NewMemoryStore()
	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend: runtime.BackendMemory,
		Policy:  policy.Fixed(nil),
	}, store, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer instance.Close()
	if _, err := instance.ListObjects(context.Background(), "bucket", runtime.ListOptions{}); err == nil {
		t.Error("a typed-nil policy permitted an operation, so the mistake fails open")
	}
}

func openWithSource(t *testing.T, rot *rotating) (*runtime.Instance, func()) {
	t.Helper()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend: runtime.BackendMemory,
		Policy:  rot.source(),
	}, store, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return instance, func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}
}
