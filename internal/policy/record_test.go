package policy_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
)

var deadline = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// clockInsideWindow is a fixed instant a sample revision is still valid at. Every
// read in this file uses a stated instant rather than time.Now, because a
// deadline is a property of the revision and these cases are about the revision,
// not about when the suite happened to run.
func clockInsideWindow() time.Time { return deadline.Add(-time.Minute) }

// mustFromSet is one allow over an object prefix with an explicit deny inside
// it, which is the shape that exercises both effects and both selector kinds in
// a single round trip.
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

func TestATamperedRecordIsRefusedRatherThanReadNarrower(t *testing.T) {
	path := writeSealed(t, mustFromSet(t, "rev-1", 1))
	// The dangerous edit is the one that makes a policy *look* tighter while
	// granting more: dropping the deadline makes a revocation permanent, and
	// removing a deny entry turns a refusal into a grant. Both must fail the
	// seal rather than take effect.
	for _, tc := range []struct{ name, from, to string }{
		{"deadline removed", `"expires":"2026-09-30T12:00:00Z"`, `"expires":"0001-01-01T00:00:00Z"`},
		{"deny entry dropped", `"effect":"deny",`, `"effect":"allow",`},
		{"grant widened", `"operations":["object.read"]`, `"operations":["object.read","object.write","object.delete"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			edited := strings.Replace(string(raw), tc.from, tc.to, 1)
			if edited == string(raw) {
				t.Fatalf("the fixture does not contain %s, so this case proves nothing", tc.from)
			}
			record, err := policy.Decode([]byte(edited))
			if err != nil {
				if !errors.Is(err, policy.ErrRecordDamaged) {
					t.Errorf("a tampered record was refused as %v, want ErrRecordDamaged", err)
				}
				return
			}
			if _, err := record.Policy(clockInsideWindow); !errors.Is(err, policy.ErrRecordDamaged) {
				t.Errorf("a tampered record passed its seal and rebuilt a policy: %v", err)
			}
		})
	}
}

func TestATruncatedOrForeignFileIsNotAPolicy(t *testing.T) {
	for name, data := range map[string]string{
		"empty":       "",
		"truncated":   `{"version":1,"entries":[{"namespace":"ns"`,
		"not json":    "permission granted",
		"future form": `{"version":99,"entries":[],"integrity":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			record, err := policy.Decode([]byte(data))
			if err == nil {
				if _, err := record.Policy(clockInsideWindow); !errors.Is(err, policy.ErrRecordDamaged) {
					t.Errorf("%s decoded and rebuilt a policy: %v", name, err)
				}
				return
			}
			if data != "" && !errors.Is(err, policy.ErrRecordDamaged) {
				t.Errorf("%s = %v, want ErrRecordDamaged", name, err)
			}
			if data == "" && !errors.Is(err, policy.ErrNoRecord) {
				t.Errorf("an absent policy = %v, want ErrNoRecord", name)
			}
		})
	}
}

func TestARecordWithoutARevisionIdentityIsNotPersisted(t *testing.T) {
	var set policy.Set
	set.Add("ns", "b", policy.Selector{Kind: policy.KindObject, Prefix: "a"}, policy.Allow, authority.ObjectRead)
	if _, err := policy.FromSet(set, 0, deadline.Add(-time.Hour), authority.All()); err == nil {
		t.Error("a policy with no host-issued revision identity was persisted")
	}
}

// TestAnEditedOperationsListFailsTheSeal covers the case a record written by a
// build with more Operations than this one would otherwise raise: widening what
// an allow entry covers. The seal is what refuses it, and it has to — an
// unknown operation is dropped on the way in, so a record that reached Authorize
// intact would silently grant less than it says, which is the direction that
// looks safe and is not the one that matters.
func TestAnEditedOperationsListFailsTheSeal(t *testing.T) {
	written := mustFromSet(t, "rev-future", 1)
	data, err := written.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw := strings.Replace(string(data), `"object.read"`, `"object.read","object.write"`, 1)
	if raw == string(data) {
		t.Fatal("the fixture does not contain the operations list, so this case proves nothing")
	}
	decoded, err := policy.Decode([]byte(raw))
	if err != nil {
		if !errors.Is(err, policy.ErrRecordDamaged) {
			t.Errorf("a widened allow entry = %v, want ErrRecordDamaged", err)
		}
		return
	}
	if _, err := decoded.Policy(clockInsideWindow); !errors.Is(err, policy.ErrRecordDamaged) {
		t.Errorf("a widened allow entry passed the seal and rebuilt a policy: %v", err)
	}
}

// writeSealed writes a record to a file and returns its path, so a case can edit
// the bytes a reader would find on disk. The file is written here rather than
// through a store because this is about what the record format refuses, and a
// store in the middle would be a second thing that could be wrong.
func writeSealed(t *testing.T, record policy.Record) string {
	t.Helper()
	data, err := record.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// TestAnOperationThisBuildDoesNotDefineIsDropped covers a record whose operations
// list names something Authority has no bit for, which is what a record from a
// future build looks like. The seal covers edits; this covers a record that is
// genuine but was written by a build with a larger operation set.
func TestAnOperationThisBuildDoesNotDefineIsDropped(t *testing.T) {
	// Sealed through the package's own writer, so the record is authentic and
	// only its contents are unusual.
	written := policy.SealForTest(t, mustFromSet(t, "rev-future", 0),
		[]authority.Operation{authority.ObjectRead, "operation.from.the.future"})
	set, err := written.Policy(clockInsideWindow)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if err := set.Allows(authority.All(), object, authority.ObjectRead); err != nil {
		t.Errorf("the operations this build does know were dropped too: %v", err)
	}
	if err := set.Allows(authority.All(), object, authority.Operation("operation.from.the.future")); err == nil {
		t.Error("an operation this build cannot name was granted")
	}
}
