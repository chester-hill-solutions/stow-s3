package runthrough

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough/outboxpb"
	"google.golang.org/protobuf/proto"
)

func nativeProtoFixture(t *testing.T) outboxState {
	t.Helper()
	state := protoOutboxFixture()
	state.seq, state.nextToken = 900, 901
	data, err := benchmarkEncodeOutboxJSON(state)
	if err != nil {
		t.Fatal(err)
	}
	state, err = decodeOutboxState(data)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestFileOutboxPersistsProtobufAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.json")
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	want := nativeProtoFixture(t)
	if err := outbox.withState(func(state *outboxState) error { *state = want; return nil }); err != nil {
		t.Fatal(err)
	}
	checkNativeProtoSnapshot(t, path, want)
}

func TestFileOutboxMigratesJSONToProtobuf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.json")
	want := nativeProtoFixture(t)
	data, err := benchmarkEncodeOutboxJSON(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.Close(); err != nil {
		t.Fatal(err)
	}
	checkNativeProtoSnapshot(t, path, want)
}

func checkNativeProtoSnapshot(t *testing.T, path string, want outboxState) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte(outboxProtoMagic)) {
		t.Fatal("native outbox still writes JSON")
	}
	reopened, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	var got outboxState
	err = reopened.withLockedState(func(state outboxState) error { got = state.clone(); return nil })
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reopen lost outbox state: got=%+v want=%+v error=%v", got, want, err)
	}
	entry, err := reopened.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "after-reopen", Version: "new-version", UpstreamAbsent: true})
	if err != nil || entry.ID != "outbox-901" {
		t.Fatalf("sequence after reopen: %+v %v", entry, err)
	}
	claimed, ok, err := reopened.Claim(entry.ID, "new-owner", time.Minute)
	if err != nil || !ok || claimed.Entry.ClaimToken != 902 {
		t.Fatalf("fence after reopen: %+v %t %v", claimed, ok, err)
	}
}

func TestFileOutboxRefusesFutureProtobufWithoutReplacement(t *testing.T) {
	data, err := encodeOutboxProto(nativeProtoFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	message := &outboxpb.Outbox{}
	if err := proto.Unmarshal(data[outboxProtoPrefixSize:], message); err != nil {
		t.Fatal(err)
	}
	message.Version++
	payload, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:outboxProtoPrefixSize], payload...)
	binary.LittleEndian.PutUint32(data[len(outboxProtoMagic):], uint32(len(payload)))
	path := filepath.Join(t.TempDir(), "outbox.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileOutbox(path); !errors.Is(err, ErrOutboxFormatVersion) {
		t.Fatalf("future schema error=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatalf("future journal replaced: %v", err)
	}
}
