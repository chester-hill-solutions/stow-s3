package policy_test

import (
	"errors"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
)

var (
	object    = policy.Resource{Namespace: "ns", Collection: "b", Kind: policy.KindObject, Locator: "a/b/c"}
	workspace = policy.Resource{Namespace: "ns", Collection: "w", Kind: policy.KindWorkspace, Locator: "a/b"}
)

func TestObjectPrefixUsesKeySemanticsAndSubtreeUsesComponentSemantics(t *testing.T) {
	abc := policy.Resource{Namespace: "ns", Collection: "b", Kind: policy.KindObject, Locator: "abc"}
	ab := policy.Resource{Namespace: "ns", Collection: "w", Kind: policy.KindWorkspace, Locator: "ab"}

	keys := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	})
	if err := keys.Allows(authority.All(), abc, authority.ObjectRead); err != nil {
		t.Errorf("object prefix a did not cover abc: %v", err)
	}
	if err := keys.Allows(authority.All(), object, authority.ObjectRead); err != nil {
		t.Errorf("object prefix a did not cover a/b/c: %v", err)
	}

	paths := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Add("ns", "w", policy.Selector{Kind: policy.KindWorkspace, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	})
	if err := paths.Allows(authority.All(), workspace, authority.ObjectRead); err != nil {
		t.Errorf("workspace subtree a did not cover a/b: %v", err)
	}
	if err := paths.Allows(authority.All(), ab, authority.ObjectRead); err == nil {
		t.Error("workspace subtree a covered the sibling ab; scope is by component")
	}
}

func TestASelectorOfOneKindNeverMatchesTheOther(t *testing.T) {
	same := policy.Resource{Namespace: "ns", Collection: "w", Kind: policy.KindObject, Locator: "a/b"}
	set := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Add("ns", "w", policy.Selector{Kind: policy.KindWorkspace, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	})
	if err := set.Allows(authority.All(), same, authority.ObjectRead); err == nil {
		t.Error("a workspace subtree granted access to an object resource")
	}
}

func TestExplicitDenyBeatsAllowAndDefaultIsDenial(t *testing.T) {
	set := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.ObjectRead)
		s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Exact: "a/b/c"}, policy.Deny, authority.ObjectRead)
	})
	if err := set.Allows(authority.All(), object, authority.ObjectRead); err == nil {
		t.Error("explicit deny did not beat a covering allow")
	}
	named := policy.Resource{Namespace: "ns", Collection: "b", Kind: policy.KindObject, Locator: "a/other"}
	if err := set.Allows(authority.All(), named, authority.ObjectRead); err != nil {
		t.Errorf("the allow was withdrawn from a resource it still covers: %v", err)
	}
	uncovered := policy.Resource{Namespace: "ns", Collection: "b", Kind: policy.KindObject, Locator: "z"}
	if err := set.Allows(authority.All(), uncovered, authority.ObjectRead); err == nil {
		t.Error("an operation no allow entry covers was permitted; the default is denial")
	}
}

func TestAnUncoveredOperationIsRefusedEvenUnderAnAllow(t *testing.T) {
	set := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	})
	if err := set.Allows(authority.All(), object, authority.ObjectDelete); err == nil {
		t.Error("an allow for one operation permitted a different one")
	}
}

func TestAPolicyNarrowsAndNeverWidensTheEnvironment(t *testing.T) {
	set := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.ObjectRead, authority.ObjectWrite)
	})
	readOnly := authority.ReadOnly()
	if err := set.Allows(readOnly, object, authority.ObjectWrite); err == nil {
		t.Error("a policy produced a write permission a read-only environment does not have")
	}
	if err := set.Allows(readOnly, object, authority.ObjectDelete); err == nil {
		t.Error("a policy produced a delete permission the environment does not have")
	}
	effective, err := set.Authorize(readOnly, object, authority.ObjectRead)
	if err != nil {
		t.Fatal(err)
	}
	if !readOnly.IsSupersetOf(effective) {
		t.Errorf("effective authority %s is not a subset of the environment %s", effective, readOnly)
	}
}

func TestNewRejectsAPolicyThatGrantsBeyondTheEnvironment(t *testing.T) {
	s := policy.Set{}
	s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.EnvironmentDestroy)
	_, err := policy.New(s, authority.ReadOnly())
	if !errors.Is(err, policy.ErrWidening) {
		t.Errorf("New with a widening grant = %v, want ErrWidening", err)
	}
}

func TestAnExpiredPolicyRefusesRatherThanExtendingTheGrant(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	set := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Now = func() time.Time { return now }
		s.Expires = now.Add(time.Hour)
		s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	})
	if err := set.Allows(authority.All(), object, authority.ObjectRead); err != nil {
		t.Fatalf("refused inside the freshness window: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := set.Authorize(authority.All(), object, authority.ObjectRead); !errors.Is(err, policy.ErrStalePolicy) {
		t.Errorf("past the deadline = %v, want ErrStalePolicy", err)
	}
}

func TestTheEnvironmentIsCheckedBeforeThePolicy(t *testing.T) {
	// A policy that permits everything the namespace allows still cannot produce
	// a permission the environment withholds, and the refusal must be the
	// environment's so a caller can tell the two apart.
	set := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow,
			authority.ObjectRead, authority.ObjectWrite, authority.ObjectDelete)
	})
	var denied *authority.ErrNotAuthorized
	err := set.Allows(authority.ReadOnly(), object, authority.EnvironmentDestroy)
	if !errors.As(err, &denied) {
		t.Fatalf("got %v, want an authority refusal", err)
	}
}

func TestAnotherCollectionAndNamespaceIsNotCovered(t *testing.T) {
	set := mustPolicy(t, authority.All(), func(s *policy.Set) {
		s.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	})
	for _, r := range []policy.Resource{
		{Namespace: "ns", Collection: "other", Kind: policy.KindObject, Locator: "a/b/c"},
		{Namespace: "elsewhere", Collection: "b", Kind: policy.KindObject, Locator: "a/b/c"},
	} {
		if err := set.Allows(authority.All(), r, authority.ObjectRead); err == nil {
			t.Errorf("a grant in ns/b was honoured for %s", r)
		}
	}
}

func mustPolicy(t *testing.T, env authority.Authority, build func(*policy.Set)) policy.Set {
	t.Helper()
	s := policy.Set{Revision: "test"}
	build(&s)
	out, err := policy.New(s, env)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return out
}
