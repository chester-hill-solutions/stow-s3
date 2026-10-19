package runthrough

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough/outboxpb"
	"google.golang.org/protobuf/proto"
)

func protoOutboxFixture() outboxState {
	s := newOutboxState()
	stamp := time.Date(2026, 10, 17, 12, 34, 56, 123456789, time.FixedZone("offset", 197*60))
	s.entries["active"] = OutboxEntry{ID: "active", Operation: OutboxCopy, Bucket: "bucket", Key: "key\n雪", SourceBucket: "source", SourceKey: "original", Version: "local", PreviousVersion: "previous", UpstreamVersion: "\"etag\"", UpstreamAbsent: true, CreatedAt: stamp, Attempts: 3, NextAttempt: stamp.Add(time.Hour), LastError: "retry\nerror", Terminal: true, ClaimOwner: "claim-owner", ClaimUntil: stamp.Add(time.Minute), ClaimToken: 123, Attempted: true, PreparedOwner: "prepared-owner", PreparedUntil: stamp.Add(time.Second), PreparedToken: 456}
	s.prepared["prepared"] = OutboxEntry{ID: "prepared", Operation: OutboxPut, Bucket: "bucket", Key: "prepared", Prepared: true, PreparedOwner: "owner", PreparedUntil: stamp, PreparedToken: 789}
	s.provenance["bucket\x00key"] = UpstreamState{ETag: "etag", Absent: true, Unknown: true}
	s.provenance["bucket\x00unknown"] = UpstreamState{}
	s.seq = ^uint64(0)
	s.nextToken = ^uint64(0)
	return s
}

func TestOutboxProtoMatchesJSONState(t *testing.T) {
	for _, mode := range []string{"empty", "complete", "invalid-utf8", "legacy-attempt"} {
		t.Run(mode, func(t *testing.T) {
			state := protoOutboxFixture()
			switch mode {
			case "empty":
				state = newOutboxState()
			case "invalid-utf8":
				state.provenance["valid-value"] = UpstreamState{ETag: "last\xff"}
				e := state.entries["active"]
				e.LastError = "bad\xff"
				state.entries["active"] = e
			case "legacy-attempt":
				e := state.entries["active"]
				e.Attempted = false
				state.entries["active"] = e
			}
			baseline, err := benchmarkEncodeOutboxJSON(state)
			if err != nil {
				t.Fatal(err)
			}
			want, err := decodeOutboxState(baseline)
			if err != nil {
				t.Fatal(err)
			}
			data, err := encodeOutboxProto(state)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeOutboxProto(data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("protobuf state differs from JSON: got=%+v want=%+v", got, want)
			}
			again, err := encodeOutboxProto(state)
			if err != nil || !bytes.Equal(data, again) {
				t.Fatalf("nondeterministic encoding: %v", err)
			}
		})
	}
}

func TestOutboxProtoRejectsIncompleteFiles(t *testing.T) {
	data, err := encodeOutboxProto(protoOutboxFixture())
	if err != nil {
		t.Fatal(err)
	}
	for cut := 0; cut < len(data); cut++ {
		if _, err := decodeOutboxProto(data[:cut]); err == nil {
			t.Fatalf("accepted truncation at %d/%d", cut, len(data))
		}
	}
	if _, err := decodeOutboxProto(append(data, 0)); err == nil {
		t.Fatal("accepted trailing data")
	}
}

func TestOutboxProtoRejectsUnknownSchema(t *testing.T) {
	for _, mode := range []string{"version", "snapshot", "provenance", "entry"} {
		t.Run(mode, func(t *testing.T) {
			message := &outboxpb.Outbox{Version: outboxProtoVersion, Provenance: []*outboxpb.Provenance{{Identity: "key"}}, Entries: []*outboxpb.Entry{{Identity: "entry"}}}
			unknown := []byte{0xf8, 0x07, 1}
			switch mode {
			case "version":
				message.Version++
			case "snapshot":
				message.ProtoReflect().SetUnknown(unknown)
			case "provenance":
				message.Provenance[0].ProtoReflect().SetUnknown(unknown)
			case "entry":
				message.Entries[0].ProtoReflect().SetUnknown(unknown)
			}
			payload, err := proto.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			data := make([]byte, outboxProtoPrefixSize+len(payload))
			copy(data, outboxProtoMagic)
			binary.LittleEndian.PutUint32(data[len(outboxProtoMagic):], uint32(len(payload)))
			copy(data[outboxProtoPrefixSize:], payload)
			if _, err := decodeOutboxProto(data); !errors.Is(err, ErrOutboxFormatVersion) {
				t.Fatalf("schema accepted or wrong cause: %v", err)
			}
		})
	}
}

func TestOutboxProtoPreservesTimestampErrors(t *testing.T) {
	s := protoOutboxFixture()
	e := s.entries["active"]
	e.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	s.entries["active"] = e
	if _, err := benchmarkEncodeOutboxJSON(s); err == nil {
		t.Fatal("JSON fixture did not reject timestamp")
	}
	if _, err := encodeOutboxProto(s); err == nil {
		t.Fatal("protobuf accepted invalid timestamp")
	}
}
