package runthrough

import (
	"fmt"
	"testing"
)

var protoBlockFrameSink []byte
var protoBlockStateSink outboxState

func BenchmarkOutboxProtoBlocks(b *testing.B) {
	for _, mixed := range []bool{false, true} {
		for _, depth := range []int{0, 128, 4096} {
			for _, operation := range []string{"encode", "decode"} {
				b.Run(fmt.Sprintf("mixed=%t/depth=%d/%s", mixed, depth, operation), func(b *testing.B) { benchmarkProtoBlocks(b, depth, mixed, operation) })
			}
		}
	}
}

func benchmarkProtoBlocks(b *testing.B, depth int, mixed bool, operation string) {
	state := protoBlockFixture(depth, mixed)
	frame, err := encodeOutboxProto(state)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if operation == "encode" {
			protoBlockFrameSink, err = encodeOutboxProto(state)
		} else {
			protoBlockStateSink, err = decodeOutboxProto(frame)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(len(frame)), "frame-B")
	if operation == "encode" {
		frame = protoBlockFrameSink
	}
	decoded, err := decodeOutboxProto(frame)
	if err != nil || len(decoded.provenance) != depth {
		b.Fatalf("frame verification: %v", err)
	}
	if mixed && (len(decoded.entries) != depth || len(decoded.prepared) != depth) {
		b.Fatal("entry count changed")
	}
	protoBlockFrameSink = nil
	protoBlockStateSink = outboxState{}
}
