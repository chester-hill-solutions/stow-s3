package policy

import (
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
)

// SealForTest produces an authentic record whose operations list is not one this
// build would have written. A record like that can only come from a build with a
// larger operation set, and the seal cannot detect it — the record is genuine, so
// re-sealing it is the honest way to construct the case — which makes the reading
// the only place that can be careful about what it does with an operation it
// cannot name.
func SealForTest(t *testing.T, record Record, operations []authority.Operation) Record {
	t.Helper()
	record.Entries = []recordEntry{{
		Namespace:  "ns",
		Collection: "b",
		Kind:       KindObject,
		Prefix:     "a",
		Effect:     Allow,
		Operations: operations,
	}}
	return record.seal()
}
