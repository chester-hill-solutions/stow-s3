package runthrough

import (
	"fmt"
	"reflect"
	"testing"
)

func cloneContractFixture(mask int) outboxState {
	s := outboxState{seq: 41, nextToken: 42}
	if mask&1 != 0 {
		s.entries = map[string]OutboxEntry{"active": {ID: "active", Operation: OutboxPut, Bucket: "bucket", Key: "active", Version: "v"}}
	}
	if mask&2 != 0 {
		s.prepared = map[string]OutboxEntry{"prepared": {ID: "prepared", Prepared: true, PreparedOwner: "worker", PreparedToken: 42}}
	}
	if mask&4 != 0 {
		s.provenance = map[string]UpstreamState{"upstream": {ETag: "etag"}}
	}
	return s
}

func TestOutboxStateCloneIndependence(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		t.Run(fmt.Sprintf("maps-%d", mask), func(t *testing.T) {
			checkOutboxCloneIndependence(t, mask)
		})
	}
	t.Run("empty-nonnil", func(t *testing.T) {
		s := newOutboxState()
		clone := s.clone()
		if !reflect.DeepEqual(s, clone) {
			t.Fatal("empty clone changed map state")
		}
		clone.entries["new"] = OutboxEntry{ID: "new"}
		if len(s.entries) != 0 {
			t.Fatal("empty maps alias")
		}
	})
}

func checkOutboxCloneIndependence(t *testing.T, mask int) {
	t.Helper()
	s, original := cloneContractFixture(mask), cloneContractFixture(mask)
	clone := s.clone()
	if clone.entries == nil || clone.prepared == nil || clone.provenance == nil {
		t.Fatal("clone must have mutable maps even for nil input")
	}
	if clone.seq != s.seq || clone.nextToken != s.nextToken {
		t.Fatal("clone changed counters")
	}
	for id, value := range s.entries {
		if clone.entries[id] != value {
			t.Fatal("active entry changed")
		}
	}
	for id, value := range s.prepared {
		if clone.prepared[id] != value {
			t.Fatal("prepared entry changed")
		}
	}
	for id, value := range s.provenance {
		if clone.provenance[id] != value {
			t.Fatal("provenance changed")
		}
	}
	clone.entries["active"] = OutboxEntry{ID: "active", Version: "changed"}
	clone.prepared["prepared"] = OutboxEntry{ID: "prepared", PreparedToken: 99}
	clone.provenance["upstream"] = UpstreamState{Absent: true}
	clone.seq, clone.nextToken = 99, 100
	if !reflect.DeepEqual(s, original) {
		t.Fatal("clone mutation changed original state")
	}
	delete(clone.entries, "active")
	delete(clone.prepared, "prepared")
	delete(clone.provenance, "upstream")
	if !reflect.DeepEqual(s, original) {
		t.Fatal("clone deletion changed original state")
	}
}
