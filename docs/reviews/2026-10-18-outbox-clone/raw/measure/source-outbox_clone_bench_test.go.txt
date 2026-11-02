package runthrough

import (
	"fmt"
	"reflect"
	"testing"
)

var outboxCloneSink outboxState

func cloneBenchFixture(depth int, churned bool) outboxState {
	s := newOutboxState()
	s.seq, s.nextToken = 41, 42
	inserted := depth
	if churned && depth > 0 {
		inserted = 32768
	}
	for n := 0; n < inserted; n++ {
		id := fmt.Sprintf("entry-%08d", n)
		s.entries[id] = OutboxEntry{ID: id, Operation: OutboxPut, Bucket: "bucket", Key: id, Version: "version"}
		s.prepared[id] = OutboxEntry{ID: id, Prepared: true, PreparedOwner: "worker", PreparedToken: 42}
		s.provenance[id] = UpstreamState{ETag: "etag"}
	}
	for n := depth; n < inserted; n++ {
		id := fmt.Sprintf("entry-%08d", n)
		delete(s.entries, id)
		delete(s.prepared, id)
		delete(s.provenance, id)
	}
	return s
}

func BenchmarkOutboxStateClone(b *testing.B) {
	for _, depth := range []int{0, 128, 4096} {
		for _, churned := range []bool{false, true} {
			b.Run(fmt.Sprintf("depth=%d/churned=%t", depth, churned), func(b *testing.B) {
				s := cloneBenchFixture(depth, churned)
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					outboxCloneSink = s.clone()
				}
				b.StopTimer()
				if !reflect.DeepEqual(s, outboxCloneSink) {
					b.Fatal("clone changed state")
				}
				outboxCloneSink = outboxState{}
			})
		}
	}
}
