package runthrough

import (
	"fmt"
	"testing"
)

func BenchmarkOutboxIdentityValidation(b *testing.B) {
	for _, depth := range []int{0, 128, 16384} {
		state := newOutboxState()
		for i := 0; i < depth; i++ {
			key := fmt.Sprintf("identity-%06d", i)
			state.provenance[key] = UpstreamState{ETag: "0123456789abcdef0123456789abcdef"}
			state.entries[key] = OutboxEntry{ID: key, Operation: OutboxPut, Bucket: "bucket", Key: key, Version: "local"}
			state.prepared["prepared/"+key] = OutboxEntry{ID: "prepared/" + key, Operation: OutboxPut, Bucket: "bucket", Key: key, Prepared: true}
		}
		for _, operation := range []string{"encode", "decode"} {
			b.Run(fmt.Sprintf("depth=%d/%s", depth, operation), func(b *testing.B) {
				data, err := encodeOutboxProto(state)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					if operation == "encode" {
						_, err = encodeOutboxProto(state)
					} else {
						_, err = decodeOutboxProto(data)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				got, err := decodeOutboxProto(data)
				if err != nil || len(got.entries) != depth || len(got.prepared) != depth || len(got.provenance) != depth {
					b.Fatalf("invalid benchmark fixture: %v", err)
				}
			})
		}
	}
}
