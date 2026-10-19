package runthrough

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func repeatedEmptyOutboxFrame(field byte, count int) []byte {
	frame := make([]byte, outboxProtoPrefixSize, outboxProtoPrefixSize+2+count*2)
	copy(frame, outboxProtoMagic)
	frame = append(frame, 8, byte(outboxProtoVersion))
	for n := 0; n < count; n++ {
		frame = append(frame, field<<3|2, 0)
	}
	binary.LittleEndian.PutUint32(frame[len(outboxProtoMagic):], uint32(len(frame)-outboxProtoPrefixSize))
	return frame
}

func TestOutboxDecodeRejectsDuplicateFloodWithinBudget(t *testing.T) {
	for _, item := range []struct {
		name  string
		field byte
	}{{"provenance", 2}, {"entries", 3}, {"prepared", 4}} {
		t.Run(item.name, func(t *testing.T) {
			data := repeatedEmptyOutboxFrame(item.field, 32768)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			state, err := decodeOutboxProto(data)
			runtime.ReadMemStats(&after)
			allocated := after.TotalAlloc - before.TotalAlloc
			fmt.Printf("STOWDECODEFLOOD: {\"collection\":%q,\"frameBytes\":%d,\"allocatedBytes\":%d}\n", item.name, len(data), allocated)
			if err == nil || !strings.Contains(err.Error(), "duplicate outbox") {
				t.Fatalf("duplicate records not rejected: %v", err)
			}
			if state.entries != nil || state.prepared != nil || state.provenance != nil {
				t.Fatal("failed decode returned partial state")
			}
			if allocated > 1<<20 {
				t.Fatal("small duplicate snapshot materialized the entire repeated collection")
			}
			runtime.KeepAlive(data)
		})
	}
}
