package runthrough

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func protoBlockFixture(depth int, mixed bool) outboxState {
	s := newOutboxState()
	s.seq, s.nextToken = uint64(depth+1), uint64(depth+1000)
	stamp := time.Date(2026, 10, 18, 1, 2, 3, 123456789, time.UTC)
	for n := 0; n < depth; n++ {
		id := fmt.Sprintf("entry-%06d", n)
		s.provenance[outboxIdentity("bucket", id)] = UpstreamState{ETag: fmt.Sprintf("etag-%d", n), Absent: n%2 == 0, Unknown: n%3 == 0}
		if !mixed {
			continue
		}
		entry := OutboxEntry{ID: id, Operation: OutboxCopy, Bucket: "bucket", Key: fmt.Sprintf("key-雪-%d", n), SourceBucket: "source", SourceKey: fmt.Sprintf("source-%d", n), Version: fmt.Sprintf("version-%d", n), PreviousVersion: "previous", UpstreamVersion: "upstream", UpstreamAbsent: n%2 == 0, CreatedAt: stamp, Attempts: n % 3, NextAttempt: stamp.Add(time.Hour), LastError: fmt.Sprintf("retry-%d", n), Terminal: n%2 == 0, ClaimOwner: "claim-owner", ClaimUntil: stamp.Add(time.Minute), ClaimToken: uint64(n + 1), Attempted: true, PreparedOwner: "prepared-owner", PreparedUntil: stamp.Add(time.Second), PreparedToken: uint64(n + 2)}
		s.entries[id] = entry
		entry.ID = "prepared-" + id
		entry.Prepared = true
		s.prepared[entry.ID] = entry
	}
	return s
}

func TestOutboxProtoBlockRoundTrip(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		for _, depth := range []int{0, 1, 2, 127, 128, 129, 257} {
			t.Run(fmt.Sprintf("mixed=%t/depth=%d", mixed, depth), func(t *testing.T) { checkProtoBlockRoundTrip(t, depth, mixed) })
		}
	}
}

func checkProtoBlockRoundTrip(t *testing.T, depth int, mixed bool) {
	t.Helper()
	state := protoBlockFixture(depth, mixed)
	original := state.clone()
	data, err := encodeOutboxProto(state)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeOutboxProto(data)
	if err != nil || !reflect.DeepEqual(got, state) {
		t.Fatalf("record values changed: %v", err)
	}
	again, err := encodeOutboxProto(state)
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("nondeterministic frame: %v", err)
	}
	if !reflect.DeepEqual(state, original) {
		t.Fatal("encoding changed caller state")
	}
	fmt.Printf("STOWPROTOFRAME: {\"name\":%q,\"sha256\":\"%x\",\"bytes\":%d}\n", t.Name(), sha256.Sum256(data), len(data))
}

func TestOutboxProtoBlockConcurrentOwnership(t *testing.T) {
	state := protoBlockFixture(129, true)
	expected, err := encodeOutboxProto(state)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		data []byte
		err  error
	}
	results := make(chan result, 8)
	for n := 0; n < 8; n++ {
		go func() { data, err := encodeOutboxProto(state); results <- result{data, err} }()
	}
	var frames [][]byte
	for n := 0; n < 8; n++ {
		row := <-results
		if row.err != nil || !bytes.Equal(row.data, expected) {
			t.Fatalf("concurrent frame changed: %v", row.err)
		}
		frames = append(frames, row.data)
	}
	frames[0][0] ^= 1
	for _, data := range frames[1:] {
		if !bytes.Equal(data, expected) {
			t.Fatal("encoded frames share mutable output")
		}
	}
}
