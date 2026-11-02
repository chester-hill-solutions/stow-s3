package runthrough

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough/outboxpb"
	"google.golang.org/protobuf/proto"
)

func identityMessage(t *testing.T) *outboxpb.Outbox {
	t.Helper()
	data, err := encodeOutboxProto(nativeProtoFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	message := &outboxpb.Outbox{}
	if err := proto.Unmarshal(data[outboxProtoPrefixSize:], message); err != nil {
		t.Fatal(err)
	}
	return message
}

func encodeIdentityMessage(t *testing.T, message *outboxpb.Outbox) []byte {
	t.Helper()
	payload, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, outboxProtoPrefixSize+len(payload))
	copy(data, outboxProtoMagic)
	binary.LittleEndian.PutUint32(data[len(outboxProtoMagic):], uint32(len(payload)))
	copy(data[outboxProtoPrefixSize:], payload)
	return data
}

func duplicateIdentityMessage(t *testing.T, collection string, identical, reverse bool) *outboxpb.Outbox {
	t.Helper()
	message := identityMessage(t)
	if collection == "provenance" {
		first := message.Provenance[0]
		second := proto.Clone(first).(*outboxpb.Provenance)
		if !identical {
			second.Etag, second.Absent, second.Unknown = "replacement-validator", !first.Absent, !first.Unknown
		}
		if reverse {
			first, second = second, first
		}
		message.Provenance = []*outboxpb.Provenance{first, second}
		return message
	}
	entries := message.Entries
	if collection == "prepared" {
		entries = message.Prepared
	}
	first := entries[0]
	second := proto.Clone(first).(*outboxpb.Entry)
	if !identical {
		second.Key, second.Version, second.ClaimOwner, second.ClaimToken = "replacement-key", "replacement-version", "replacement-owner", 42
	}
	if reverse {
		first, second = second, first
	}
	entries = []*outboxpb.Entry{first, second}
	if collection == "prepared" {
		message.Prepared = entries
	} else {
		message.Entries = entries
	}
	return message
}

func identityCases(t *testing.T, check func(*testing.T, string, []byte)) {
	t.Helper()
	for _, collection := range []string{"entries", "prepared", "provenance"} {
		for _, identical := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/identical=%t/reverse=%t", collection, identical, reverse), func(t *testing.T) {
					check(t, collection, encodeIdentityMessage(t, duplicateIdentityMessage(t, collection, identical, reverse)))
				})
			}
		}
	}
}

func TestOutboxProtoRejectsDuplicateIdentities(t *testing.T) {
	identityCases(t, func(t *testing.T, _ string, data []byte) {
		if state, err := decodeOutboxProto(data); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate identity accepted: state=%+v error=%v", state, err)
		}
	})
}

func TestFileOutboxRefusesDuplicateProtoIdentityOnOpen(t *testing.T) {
	identityCases(t, func(t *testing.T, collection string, data []byte) {
		path := filepath.Join(t.TempDir(), "outbox")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		outbox, err := NewFileOutbox(path)
		if err == nil {
			pending, pErr := outbox.PendingSnapshot()
			prepared, rErr := outbox.PreparedSnapshot()
			closeErr := outbox.Close()
			after, readErr := os.ReadFile(path)
			t.Fatalf("accepted duplicate %s: pending=%+v prepared=%+v snapshotErrors=%v/%v close=%v rewritten=%t read=%v", collection, pending, prepared, pErr, rErr, closeErr, !bytes.Equal(data, after), readErr)
		}
		requireMigrationBytes(t, path, data)
	})
}

func TestFileOutboxRefusesDuplicateProtoIdentityOnReload(t *testing.T) {
	for _, collection := range []string{"entries", "prepared", "provenance"} {
		for _, operation := range []string{"pending", "prepared", "lookup", "enqueue", "prepare", "save", "close"} {
			t.Run(collection+"/"+operation, func(t *testing.T) { checkDuplicateIdentityReload(t, collection, operation) })
		}
	}
}

func checkDuplicateIdentityReload(t *testing.T, collection, operation string) {
	t.Helper()
	original := encodeIdentityMessage(t, identityMessage(t))
	bad := encodeIdentityMessage(t, duplicateIdentityMessage(t, collection, false, false))
	path := filepath.Join(t.TempDir(), "outbox")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	var want outboxState
	if err := outbox.withLockedState(func(state outboxState) error { want = state.clone(); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrationOperation(outbox, operation); err == nil {
		t.Errorf("%s accepted duplicate %s", operation, collection)
	}
	requireMigrationBytes(t, path, bad)
	if !reflect.DeepEqual(outbox.Pending(), want.pending()) || !reflect.DeepEqual(outbox.Prepared(), want.preparedEntries()) {
		t.Error("last acknowledged state changed")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := outbox.withLockedState(func(state outboxState) error {
		if !reflect.DeepEqual(state, want) {
			t.Errorf("restored state differs: %+v", state)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entry, err := outbox.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "after-repair", Version: "local"})
	if err != nil || entry.ID != "outbox-901" {
		t.Fatalf("recovery sequence: %+v %v", entry, err)
	}
}

func TestOutboxProtoRefusesLossyIdentityEncoding(t *testing.T) {
	for _, collection := range []string{"entries", "prepared", "provenance"} {
		for _, collision := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/collision=%t", collection, collision), func(t *testing.T) {
				state := nativeProtoFixture(t)
				if collection == "provenance" {
					state.provenance["\xfe"] = UpstreamState{ETag: "first"}
					if collision {
						state.provenance["\xff"] = UpstreamState{ETag: "second"}
					}
				} else {
					entries := state.entries
					if collection == "prepared" {
						entries = state.prepared
					}
					entries["\xfe"] = OutboxEntry{ID: "first", Prepared: collection == "prepared"}
					if collision {
						entries["\xff"] = OutboxEntry{ID: "second", Prepared: collection == "prepared"}
					}
				}
				data, err := encodeOutboxProto(state)
				if err == nil {
					got, decodeErr := decodeOutboxProto(data)
					t.Fatalf("lossy identity encoding accepted: before=%+v after=%+v decode=%v", state, got, decodeErr)
				}
			})
		}
	}
}

func TestFileOutboxRefusesLossyProvenanceIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox")
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	want := UpstreamState{ETag: "safe-validator"}
	if err := outbox.SaveUpstreamState("bucket", "safe", want); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.SaveUpstreamState("bucket", "\xff", UpstreamState{ETag: "unsafe"}); err == nil {
		t.Error("accepted lossy provenance identity")
	}
	requireMigrationBytes(t, path, original)
	got, exists, err := outbox.UpstreamState("bucket", "safe")
	if err != nil || !exists || got != want {
		t.Fatalf("safe state changed: %+v %t %v", got, exists, err)
	}
	if err := outbox.SaveUpstreamState("bucket", "recovered", want); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxProtoPreservesDistinctIdentities(t *testing.T) {
	state := newOutboxState()
	for _, key := range []string{"", "a", "a\x00b", "雪", "\ufffd", "\ufffd\ufffd"} {
		state.provenance[key] = UpstreamState{ETag: key}
		state.entries["active/"+key] = OutboxEntry{ID: "active/" + key, Operation: OutboxPut, Key: key}
		state.prepared["prepared/"+key] = OutboxEntry{ID: "prepared/" + key, Operation: OutboxPut, Key: key, Prepared: true}
	}
	data, err := encodeOutboxProto(state)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeOutboxProto(data)
	if err != nil || !reflect.DeepEqual(got, state) {
		t.Fatalf("distinct identities changed: %+v %v", got, err)
	}
}
