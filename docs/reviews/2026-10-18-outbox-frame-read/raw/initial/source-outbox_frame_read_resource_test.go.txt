package runthrough

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestFileOutboxFrameReadResourceBudget(t *testing.T) {
	for _, item := range []struct {
		name     string
		declared uint32
		size     int64
		payload  []byte
		message  string
	}{
		{"short-declaration", 2, 8 << 20, []byte{8, 4}, "length"},
		{"long-declaration", ^uint32(0), 8 << 20, []byte{8, 4}, "length"},
		{"truncated-header", 2, 10, nil, "prefix"},
		{"invalid-wire", 2, 14, []byte{255, 255}, "decode protobuf"},
	} {
		t.Run(item.name, func(t *testing.T) {
			outbox, valid, id := frameReadFixture(t)
			frame := make([]byte, outboxProtoPrefixSize)
			copy(frame, outboxProtoMagic)
			binary.LittleEndian.PutUint32(frame[len(outboxProtoMagic):], item.declared)
			frame = append(frame, item.payload...)
			if err := os.WriteFile(outbox.path, frame, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(outbox.path, item.size); err != nil {
				t.Fatal(err)
			}
			original := outbox.state.clone()
			cached := append([]byte(nil), outbox.validatedData...)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := outbox.PendingSnapshot()
			runtime.ReadMemStats(&after)
			allocated := after.TotalAlloc - before.TotalAlloc
			fmt.Printf("STOWFRAMEREAD: {\"name\":%q,\"fileBytes\":%d,\"declaredBytes\":%d,\"allocatedBytes\":%d}\n", item.name, item.size, item.declared, allocated)
			if err == nil || !strings.Contains(err.Error(), item.message) {
				t.Fatalf("wrong corrupt-frame error: %v", err)
			}
			if allocated > 1<<20 {
				t.Error("rejected frame materialized the oversized file before checking length")
			}
			if !reflect.DeepEqual(original, outbox.state) || !bytes.Equal(cached, outbox.validatedData) {
				t.Error("failed reload changed validated state")
			}
			info, statErr := os.Stat(outbox.path)
			if statErr != nil || info.Size() != item.size {
				t.Fatal("failed reload changed corrupt file")
			}
			checkRestoredOutbox(t, outbox, valid, id)
		})
	}
}

func frameReadFixture(t *testing.T) (*FileOutbox, []byte, string) {
	t.Helper()
	outbox, err := NewFileOutbox(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := outbox.Enqueue(OutboxEntry{Bucket: "bucket", Key: "key", Operation: OutboxPut})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.PendingSnapshot(); err != nil {
		t.Fatal(err)
	}
	valid, err := os.ReadFile(outbox.path)
	if err != nil {
		t.Fatal(err)
	}
	return outbox, valid, entry.ID
}
