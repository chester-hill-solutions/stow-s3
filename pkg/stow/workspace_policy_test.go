package stow_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// Destroy consulted the environment only, so a policy could not deny it and a
// KindWorkspace selector could not be reached at all. Destroy is also the only
// irreversible operation here, so it is worth being exact about.
func TestAPolicyCanDenyDestroyWhereTheEnvironmentGrantsIt(t *testing.T) {
	ws := openPolicyWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-deny"
		theWholeWorkspace(s, id, policy.Deny, authority.EnvironmentDestroy)
	})
	err := ws.Destroy(context.Background())
	if err == nil {
		t.Fatal("Destroy succeeded for a workspace a policy denies")
	}
	// The reason is the check: a message naming the environment would send the
	// caller to its Authority, and the environment grants everything here.
	if !strings.Contains(err.Error(), "policy in force") {
		t.Errorf("Destroy = %v, want a refusal naming the policy as the thing that refused", err)
	}
	if !strings.Contains(err.Error(), "environment grants: all") {
		t.Errorf("Destroy = %v, want the message to report what the environment grants", err)
	}
}

// Identical to the case above but for the effect, so the previous test cannot pass
// for any reason the workspace happened to be undestroyable.
func TestTheSamePolicyWithAllowLeavesDestroyWorking(t *testing.T) {
	ws := openPolicyWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-allow"
		theWholeWorkspace(s, id, policy.Allow, authority.EnvironmentDestroy)
	})
	if err := ws.Destroy(context.Background()); err != nil {
		t.Errorf("Destroy = %v under a policy that allows it", err)
	}
}

func TestAPolicyIsACompleteStatementAboutAKindNotAPatch(t *testing.T) {
	// "Deny this one" is not "deny this one and permit everything else"; only the
	// second is a policy. The first version of this test asserted the first
	// reading and the code was right to refuse it.
	ws := openPolicyWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-other"
		s.Add(policy.LocalNamespace, id+"-elsewhere",
			policy.Selector{Kind: policy.KindWorkspace}, policy.Deny, authority.EnvironmentDestroy)
	})
	if err := ws.Destroy(context.Background()); err == nil {
		t.Error("a policy naming only another workspace permitted this one")
	}
	allowed := openPolicyWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-both"
		s.Add(policy.LocalNamespace, id+"-elsewhere",
			policy.Selector{Kind: policy.KindWorkspace}, policy.Deny, authority.EnvironmentDestroy)
		theWholeWorkspace(s, id, policy.Allow, authority.EnvironmentDestroy)
	})
	if err := allowed.Destroy(context.Background()); err != nil {
		t.Errorf("Destroy = %v for the workspace the policy explicitly allows", err)
	}
}

func TestAWholeWorkspaceDestroyIsNotNarrowedToAGrantedSubtree(t *testing.T) {
	// Narrowing an irreversible operation to part of its target is not safe, so the
	// root is refused rather than filtered.
	ws := openPolicyWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-subtree"
		s.Add(policy.LocalNamespace, id,
			policy.Selector{Kind: policy.KindWorkspace, Prefix: "src"}, policy.Allow, authority.EnvironmentDestroy)
	})
	if err := ws.Destroy(context.Background()); err == nil {
		t.Error("a subtree grant permitted a whole-workspace destroy")
	}
}

func TestAPolicyThatSaysNothingAboutWorkspacesStillRefusesDestroy(t *testing.T) {
	// Default denial is not scoped to the entries a policy happens to carry, so an
	// object-only policy refuses a workspace destroy. Letting a policy constrain only the
	// kinds it names is the tempting fix and it is a widening: a policy naming no object
	// entries would then grant every object operation.
	ws := openPolicyWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-objects"
		s.Add(policy.LocalNamespace, "some-bucket",
			policy.Selector{Kind: policy.KindObject, Prefix: "src/"}, policy.Allow,
			authority.ObjectRead, authority.ObjectWrite)
	})
	err := ws.Destroy(context.Background())
	if err == nil {
		t.Fatal("an object-only policy permitted a workspace destroy")
	}
	if !strings.Contains(err.Error(), "policy in force") {
		t.Errorf("Destroy = %v, want the policy named as the thing that refused", err)
	}
}

func TestAPolicyNamingNoKindGrantsNothingAtAll(t *testing.T) {
	// The assertion the widening above would fail: no matching entry is a refusal.
	var set policy.Set
	set.Revision = "rev-unrelated"
	set.Add(policy.LocalNamespace, "bucket",
		policy.Selector{Kind: policy.KindObject, Prefix: "src/"}, policy.Allow, authority.ObjectRead)
	if err := set.Allows(authority.All(), policy.Workspace("ws-1", ""), authority.EnvironmentDestroy); err == nil {
		t.Error("a policy with no workspace entry granted a workspace operation")
	}
	if err := set.Allows(authority.All(), policy.Object("bucket", "src/a"), authority.ObjectRead); err != nil {
		t.Errorf("the kind the policy does address was refused: %v", err)
	}
}

func TestDestroyStillNeedsTheEnvironmentGrant(t *testing.T) {
	// A policy that grants destroy cannot conjure a grant the environment withheld,
	// and the environment's answer comes first.
	ws := openPolicyWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-allow"
		theWholeWorkspace(s, id, policy.Allow, authority.EnvironmentDestroy)
	}, stow.WorkspaceOptions{Authority: readonly()})
	err := ws.Destroy(context.Background())
	if err == nil {
		t.Fatal("Destroy succeeded under a read-only environment because a policy granted it")
	}
	if strings.Contains(err.Error(), "policy in force") {
		t.Errorf("Destroy = %v, want the environment named as the thing that refused", err)
	}
}

func TestNoPolicyMeansDestroyBehavesAsItDid(t *testing.T) {
	// No directory is created first, so stow owns the workspace rather than
	// adopting one, and Destroy does what it did before any of this existed.
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
		Dir:         filepath.Join(t.TempDir(), "task"),
		RegistryDir: filepath.Join(t.TempDir(), "registry"),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := ws.Destroy(context.Background()); err != nil {
		t.Errorf("Destroy = %v without a policy, which is the pre-existing behaviour", err)
	}
}

// openPolicyWorkspace opens a workspace carrying a policy and hands build the
// generated ID. The ID cannot be named before the workspace exists, so a host does
// what this does: open, learn the registered identity, write the policy naming it,
// and reopen. The policy is a Source, so that reopen is where it takes effect.
func openPolicyWorkspace(t *testing.T, build func(*policy.Set, string), extra ...stow.WorkspaceOptions) *stow.Workspace {
	t.Helper()
	dir, registry := filepath.Join(t.TempDir(), "task"), filepath.Join(t.TempDir(), "registry")
	first, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: dir, RegistryDir: registry})
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	id := first.ID()
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	var set policy.Set
	build(&set, id)
	options := stow.WorkspaceOptions{Dir: dir, RegistryDir: registry, Policy: policy.Fixed(&set)}
	if len(extra) > 0 {
		options.Authority = extra[0].Authority
	}
	ws, err := stow.OpenWorkspace(options)
	if err != nil {
		t.Fatalf("reopen workspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func readonly() *stow.Authority {
	a := stow.ReadOnly()
	return &a
}

// theWholeWorkspace names a workspace rather than a path inside it, spelt as a
// selector with no locator.
func theWholeWorkspace(s *policy.Set, id string, effect policy.Effect, ops ...authority.Operation) {
	s.Add(policy.LocalNamespace, id, policy.Selector{Kind: policy.KindWorkspace}, effect, ops...)
}
