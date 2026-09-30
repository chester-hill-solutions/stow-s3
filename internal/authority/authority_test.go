package authority_test

import (
	"errors"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
)

func TestAllAndNone(t *testing.T) {
	if !authority.All().Allows(authority.ObjectWrite) {
		t.Fatal("All must permit object.write")
	}
	if authority.None().Allows(authority.ObjectRead) {
		t.Fatal("None must permit nothing")
	}
}

func TestAnUndefinedOperationIsDenied(t *testing.T) {
	if authority.All().Allows(authority.Operation("object.teleport")) {
		t.Fatal("an operation nobody defined must not be granted by All")
	}
}

func TestAllCoversEveryDefinedOperation(t *testing.T) {
	for _, op := range authority.Defined() {
		if !authority.All().Allows(op) {
			t.Errorf("All() does not permit the defined operation %q", op)
		}
		if _, ok := authority.OperationBit(op); !ok {
			t.Errorf("%q has no bit", op)
		}
	}
}

func TestWithAndWithout(t *testing.T) {
	ro := authority.All().Without(authority.ObjectWrite, authority.ObjectDelete, authority.BucketDelete)
	if !ro.Allows(authority.ObjectRead) || !ro.Allows(authority.ObjectWrite) == false {
		t.Fatal("read-only should still read")
	}
	if ro.Allows(authority.ObjectWrite) {
		t.Error("Without did not remove object.write")
	}
	if ro.Allows(authority.ObjectDelete) {
		t.Error("Without did not remove object.delete")
	}
	if !ro.Allows(authority.BucketCreate) {
		t.Error("Without removed an operation it was not asked to remove")
	}
}

func TestAttenuationIsOneWay(t *testing.T) {
	parent := authority.All().With(authority.UpstreamRead)
	child := parent.Without(authority.UpstreamRead, authority.ObjectDelete)

	if !parent.IsSupersetOf(child) {
		t.Error("a weaker child must be a subset of its parent")
	}
	if child.IsSupersetOf(parent) {
		t.Error("a child must never be a superset of its parent")
	}
	if !parent.Allows(authority.ObjectDelete) {
		t.Error("the parent should be unchanged by deriving a child")
	}
}

func TestIntersectIsTheNarrowerOfTheTwo(t *testing.T) {
	readOnly := authority.ReadOnly()
	// Add an operation the read-only set withholds, so the two sides differ and
	// a With-based intersection would visibly widen rather than quietly match.
	wide := readOnly.With(authority.UpstreamRead)
	both := readOnly.Intersect(wide)
	if !both.IsSupersetOf(readOnly) || both.Allows(authority.UpstreamRead) {
		t.Errorf("intersection gained an operation neither narrowing side should add: %s", both)
	}
	if both != readOnly {
		t.Errorf("intersecting with a superset = %s, want the narrower set %s", both, readOnly)
	}
	if got := readOnly.Intersect(authority.None()); got != authority.None() {
		t.Errorf("intersecting with none = %s, want none", got)
	}
	// Order must not matter, or a caller could get a different answer by
	// swapping the two sides.
	if wide.Intersect(readOnly) != both {
		t.Error("intersection depends on which side is the receiver")
	}
}

func TestCheckNamesTheRefusedOperation(t *testing.T) {
	err := authority.None().Check(authority.EnvironmentDestroy)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	var denied *authority.ErrNotAuthorized
	if !errors.As(err, &denied) {
		t.Fatalf("err = %T, want *ErrNotAuthorized", err)
	}
	if denied.Operation != authority.EnvironmentDestroy {
		t.Errorf("refused %q, want environment.destroy", denied.Operation)
	}
	if err := authority.All().Check(authority.EnvironmentDestroy); err != nil {
		t.Errorf("All must permit environment.destroy: %v", err)
	}
}

func TestOperationsAreSortedAndStable(t *testing.T) {
	first := authority.All().Without(authority.ObjectWrite).Operations()
	for i := 1; i < len(first); i++ {
		if first[i-1] >= first[i] {
			t.Fatalf("not sorted at %d: %q then %q", i, first[i-1], first[i])
		}
	}
	if len(first) != len(authority.Defined())-1 {
		t.Errorf("Operations() = %d entries, want %d", len(first), len(authority.Defined())-1)
	}
}

func TestString(t *testing.T) {
	if got := authority.All().String(); got != "all" {
		t.Errorf("All().String() = %q", got)
	}
	if got := authority.None().String(); got != "none" {
		t.Errorf("None().String() = %q", got)
	}
	if got := authority.None().With(authority.ObjectRead).String(); got != "object.read" {
		t.Errorf("String() = %q, want object.read", got)
	}
}
