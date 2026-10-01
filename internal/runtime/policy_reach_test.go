package runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// A policy entry naming an operation the runtime does not route through a check does
// nothing, silently — a deny an author wrote and that does not apply, which is worse than
// one they never wrote.
//
// It happened once: a policy denying bucket.create over an object selector was ignored,
// because check() consulted the environment only. No test failed, because
// TestObjectChecksNameAResource checks a different thing — that object operations reach
// the resource-scoped check — and says nothing about the rest.
func TestEveryOperationReachesThePolicy(t *testing.T) {
	// Each case performs the verb that carries one denied operation, so the
	// assertion is about the whole path. resourceScoped marks the operations a
	// policy scopes by selector: for those silence does not leave the answer alone,
	// because default denial is the contract.
	cases := map[authority.Operation]reachCase{}
	scope := func(perform func(context.Context, *runtime.Instance) error) reachCase {
		return reachCase{perform: perform, resourceScoped: true}
	}
	scopeOnly := func(perform func(context.Context, *runtime.Instance) error) reachCase {
		return reachCase{perform: perform}
	}
	for op, perform := range map[authority.Operation]func(context.Context, *runtime.Instance) error{
		authority.ObjectRead: func(ctx context.Context, in *runtime.Instance) error {
			_, err := in.GetObject(ctx, "bucket", "key")
			return err
		},
		authority.ObjectWrite: func(ctx context.Context, in *runtime.Instance) error {
			_, err := in.PutObject(ctx, "bucket", "key", nil, runtime.PutOptions{})
			return err
		},
		authority.ObjectDelete: func(ctx context.Context, in *runtime.Instance) error {
			return in.DeleteObject(ctx, "bucket", "key")
		},
		authority.ObjectList: func(ctx context.Context, in *runtime.Instance) error {
			_, err := in.ListObjects(ctx, "bucket", runtime.ListOptions{})
			return err
		},
	} {
		cases[op] = scope(perform)
	}
	for op, perform := range map[authority.Operation]func(context.Context, *runtime.Instance) error{
		authority.BucketList: func(ctx context.Context, in *runtime.Instance) error {
			_, err := in.HeadBucket(ctx, "bucket")
			return err
		},
		authority.BucketCreate: func(ctx context.Context, in *runtime.Instance) error {
			return in.CreateBucket(ctx, "bucket")
		},
		authority.BucketDelete: func(ctx context.Context, in *runtime.Instance) error {
			return in.DeleteBucket(ctx, "bucket")
		},
		authority.EnvironmentReset: func(ctx context.Context, in *runtime.Instance) error {
			return in.Reset(ctx)
		},
	} {
		cases[op] = scopeOnly(perform)
	}
	// Operations no runtime verb carries, each recording where it is enforced:
	// "not here" and "nowhere" are different facts and only the reason separates
	// them.
	// Every operation in authority.EnforcedElsewhere must appear here too, so the
	// machine-checked list cannot be used without this table knowing. The two answer
	// different questions: that one names a site and proves it consults the operation,
	// while this map also carries operations enforced nowhere.
	elsewhere := map[authority.Operation]string{
		authority.EnvironmentDestroy: "enforced by workspace.Destroy before lifecycle or registry mutation; see authority.Ungated",
		authority.EnvironmentPromote: "no operation is implemented; see authority.Ungated",
		authority.UpstreamRead:       "enforced by the run-through adapter at its upstream chokepoint, not by the runtime",
		authority.UpstreamWrite:      "enforced by the run-through adapter at its propagation funnel, not by the runtime",
		authority.WorkspaceCapture:   "enforced by workspace.CreateCheckpoint; site checked by authority.EnforcedElsewhere",
	}
	for op := range authority.EnforcedElsewhere {
		if _, listed := elsewhere[op]; !listed {
			t.Errorf("%s is recorded in authority.EnforcedElsewhere but this table does not know it, "+
				"so nothing here would notice if that record became wrong", op)
		}
	}
	for op := range cases {
		if _, excused := elsewhere[op]; excused {
			t.Errorf("%s is both covered by a case and listed as enforced elsewhere", op)
		}
		delete(elsewhere, op)
	}
	for op := range elsewhere {
		if _, covered := cases[op]; covered {
			t.Errorf("%s is both covered by a case and listed as enforced elsewhere", op)
		}
	}
	for _, op := range authority.Defined() {
		if _, covered := cases[op]; covered {
			continue
		}
		if reason, excused := elsewhere[op]; excused {
			if reason == "" {
				t.Errorf("operation %q is excused with no reason, which is the omission this test exists to stop", op)
			}
			continue
		}
		t.Errorf("operation %q has no case here, so nothing asserts a policy reaches it. "+
			"Add the verb that carries it, or record where it is enforced instead", op)
	}
	for op, tc := range cases {
		t.Run(string(op), func(t *testing.T) {
			assertPolicyIsTheRefusal(t, op, tc.perform)
			if !tc.resourceScoped {
				assertSilenceLeavesTheAnswerAlone(t, op, tc.perform)
			}
		})
	}
}

// assertPolicyIsTheRefusal requires the verb to be refused by the policy rather than by
// the environment, so a test cannot pass on a refusal from the wrong source.
func assertPolicyIsTheRefusal(t *testing.T, op authority.Operation, perform func(context.Context, *runtime.Instance) error) {
	t.Helper()
	instance, closeIt := openReach(t, denying(op))
	defer closeIt()
	err := perform(context.Background(), instance)
	if err == nil {
		t.Fatalf("%s was permitted by a policy that denies it", op)
	}
	if !policy.IsRefusal(err) {
		t.Fatalf("%s was refused as %v, which does not name the policy's denial", op, err)
	}
}

// assertSilenceLeavesTheAnswerAlone is the other half, and stops the fix being a
// widening the other way: a policy saying nothing about an operation must leave the
// environment's answer standing, or a policy about object keys would quietly
// narrow the namespace it never mentioned.
func assertSilenceLeavesTheAnswerAlone(t *testing.T, op authority.Operation, perform func(context.Context, *runtime.Instance) error) {
	t.Helper()
	instance, closeIt := openReach(t, silentAbout(op))
	defer closeIt()
	err := perform(context.Background(), instance)
	if policy.IsRefusal(err) {
		t.Fatalf("a policy silent about %s denied it anyway: %v", op, err)
	}
	if errors.Is(err, policy.ErrWidening) {
		t.Fatalf("a policy silent about %s was refused as widening: %v", op, err)
	}
}

// reachCase is one operation, the verb carrying it, and whether a policy scopes it.
type reachCase struct {
	perform        func(context.Context, *runtime.Instance) error
	resourceScoped bool
}

// reachPolicy names the operation a fixture withholds and the one it grants. Both
// entries sit on an object prefix, so the selector is never what makes a case pass.
type reachPolicy struct {
	deny  authority.Operation
	allow authority.Operation
}

func denying(op authority.Operation) reachPolicy {
	return reachPolicy{deny: op, allow: op}
}

func silentAbout(op authority.Operation) reachPolicy {
	return reachPolicy{allow: authority.ObjectRead, deny: authority.Operation("policy.denies.nothing")}
}

func openReach(t *testing.T, p reachPolicy) (*runtime.Instance, func()) {
	t.Helper()
	store := storage.NewMemoryStore()
	selector := policy.Selector{Kind: policy.KindObject, Prefix: "key"}
	var set policy.Set
	set.Revision = "reach"
	set.Add("runtime", "bucket", selector, policy.Allow, p.allow)
	if p.deny != "" {
		set.Add("runtime", "bucket", selector, policy.Deny, p.deny)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend: runtime.BackendMemory,
		Policy:  policy.Fixed(&set),
	}, store, func() (storage.Store, error) { return storage.NewMemoryStore(), nil })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return instance, func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}
}

// A widening policy refuses every operation, which the contract says in those
// words. It did not: check() consulted the environment only, so an Instance opened
// with a wider policy refused every object operation with ErrWidening while Reset and
// the bucket half of the namespace carried on under the environment. The answer was
// still safe, but the reason a caller saw differed by verb.
func TestAWideningPolicyRefusesEveryOperationNotJustTheOnesOnAResource(t *testing.T) {
	readOnly := authority.ReadOnly()
	store := storage.NewMemoryStore()
	var set policy.Set
	set.Revision = "widening"
	// Wider than the environment: the environment has no ObjectWrite.
	set.Add("runtime", "bucket", policy.Selector{Kind: policy.KindObject, Prefix: "key"}, policy.Allow, authority.ObjectWrite)
	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend:   runtime.BackendMemory,
		Authority: &readOnly,
		Policy:    policy.Fixed(&set),
	}, store, func() (storage.Store, error) { return storage.NewMemoryStore(), nil })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer instance.Close()
	ctx := context.Background()
	for name, perform := range map[string]func() error{
		"Reset":        func() error { return instance.Reset(ctx) },
		"CreateBucket": func() error { return instance.CreateBucket(ctx, "bucket") },
		"HeadBucket":   func() error { _, err := instance.HeadBucket(ctx, "bucket"); return err },
		"GetObject":    func() error { _, err := instance.GetObject(ctx, "bucket", "key"); return err },
	} {
		if err := perform(); !errors.Is(err, policy.ErrWidening) {
			t.Errorf("%s under a widening policy = %v, want ErrWidening", name, err)
		}
	}
}

// The environment's refusal and the policy's used to be the same type, so telling them
// apart meant reading the message. These use policy.IsRefusal.
