package runthrough

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough/outboxpb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func interleavedIdentityFrame(t testing.TB, depth int, duplicate protowire.Number) []byte {
	t.Helper()
	payload := decodeVarint(1, outboxProtoVersion)
	for n := 0; n < depth; n++ {
		for _, field := range []protowire.Number{2, 3, 4} {
			payload = append(payload, identityField(t, field, n, false)...)
		}
	}
	if duplicate != 0 {
		payload = append(payload, identityField(t, duplicate, depth/2, true)...)
		payload = append(payload, identityField(t, duplicate, depth, false)...)
	}
	return decodeFrame(payload)
}
func identityField(t testing.TB, field protowire.Number, index int, replacement bool) []byte {
	t.Helper()
	id := fmt.Sprintf("identity-%d-%06d", field, index)
	var message proto.Message
	if field == 2 {
		message = &outboxpb.Provenance{Identity: id, Etag: fmt.Sprint(replacement)}
	} else {
		version := "original"
		if replacement {
			version = "replacement"
		}
		message = &outboxpb.Entry{Identity: id, Id: id, Operation: "put", Bucket: "bucket", Key: id, Version: version, Prepared: field == 4}
	}
	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return decodeField(field, data)
}
func TestFileOutboxInterleavedDuplicateRecovery(t *testing.T) {
	for _, depth := range []int{129, 1025} {
		for _, field := range []protowire.Number{2, 3, 4} {
			t.Run(fmt.Sprintf("depth=%d/field=%d", depth, field), func(t *testing.T) {
				checkInterleavedDuplicateRecovery(t, depth, field)
			})
		}
	}
}

func checkInterleavedDuplicateRecovery(t *testing.T, depth int, field protowire.Number) {
	t.Helper()
	valid := interleavedIdentityFrame(t, depth, 0)
	checkDecodeEquivalent(t, valid)
	path := filepath.Join(t.TempDir(), "outbox")
	if err := os.WriteFile(path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	want := outbox.state.clone()
	invalid := interleavedIdentityFrame(t, depth, field)
	checkDecodeEquivalent(t, invalid)
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.PendingSnapshot(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate replacement accepted: %v", err)
	}
	if !reflect.DeepEqual(want, outbox.state) {
		t.Fatal("failed reload changed known state")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, invalid) {
		t.Fatal("failed reload changed corrupt bytes")
	}
	if err := os.WriteFile(path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	checkInterleavedQueues(t, outbox, depth, want)
}

func checkInterleavedQueues(t *testing.T, outbox *FileOutbox, depth int, want outboxState) {
	t.Helper()
	active, err := outbox.PendingSnapshot()
	if err != nil || len(active) != depth {
		t.Fatalf("restored active queue: %d, %v", len(active), err)
	}
	prepared, err := outbox.PreparedSnapshot()
	if err != nil || len(prepared) != depth {
		t.Fatalf("restored prepared queue: %d, %v", len(prepared), err)
	}
	if !reflect.DeepEqual(want, outbox.state) {
		t.Fatal("recovery changed retained state")
	}
}
