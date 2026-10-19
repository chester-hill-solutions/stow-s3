package runthrough

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
	"runtime"
	"strings"
	"testing"
)

func decodeBoundaryAllocated(t *testing.T, data []byte, reject bool) uint64 {
	t.Helper()
	const samples = 256
	_, _ = decodeOutboxProto(data)
	runtime.GC()
	var before, after runtime.MemStats
	var state outboxState
	var err error
	runtime.ReadMemStats(&before)
	for n := 0; n < samples; n++ {
		state, err = decodeOutboxProto(data)
	}
	runtime.ReadMemStats(&after)
	if (err != nil) != reject {
		t.Fatalf("unexpected boundary result: %v", err)
	}
	if reject && (state.entries != nil || state.prepared != nil || state.provenance != nil || !strings.Contains(err.Error(), "duplicate")) {
		t.Fatal("duplicate did not return an empty failed state")
	}
	runtime.KeepAlive(state)
	return (after.TotalAlloc - before.TotalAlloc) / samples
}

func TestOutboxDuplicateBoundaryAllocation(t *testing.T) {
	for _, field := range []protowire.Number{2, 3, 4} {
		t.Run(fmt.Sprintf("field=%d", field), func(t *testing.T) {
			valid := interleavedIdentityFrame(t, 8, 0)
			duplicate := interleavedIdentityFrame(t, 8, field)
			baseline := decodeBoundaryAllocated(t, valid, false)
			rejected := decodeBoundaryAllocated(t, duplicate, true)
			extra := int64(rejected) - int64(baseline)
			fmt.Printf("STOWINSERTBOUNDARY: {\"field\":%d,\"validBytesPerDecode\":%d,\"duplicateBytesPerDecode\":%d,\"extraBytes\":%d}\n", field, baseline, rejected, extra)
			if extra > 640 {
				t.Error("duplicate rejection rebuilt the full small-map prefix")
			}
		})
	}
}
