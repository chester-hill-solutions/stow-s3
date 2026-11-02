package runthrough

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFileOutboxReleasesCallerStringBuffers(t *testing.T) {
	outbox, err := NewFileOutbox(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for n := 0; n < 64; n++ {
		if err := enqueueCallerStringView(outbox, n); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := outbox.UpstreamState("bucket", "unused"); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if len(outbox.state.entries) != 64 {
		t.Fatal("retained entries lost")
	}
	result := map[string]uint64{"entries": 64, "sourceBytesPerEntry": 1 << 20, "heapBefore": before.HeapAlloc, "heapAfter": after.HeapAlloc, "heapGrowth": after.HeapAlloc - before.HeapAlloc, "cachedLength": uint64(len(outbox.validatedData))}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("STOWRETENTION:%s\n", encoded)
	if after.HeapAlloc > before.HeapAlloc && after.HeapAlloc-before.HeapAlloc > 16<<20 {
		t.Fatalf("small persisted entries retain oversized caller buffers: %s", encoded)
	}
	runtime.KeepAlive(outbox)
}
func enqueueCallerStringView(outbox *FileOutbox, n int) error {
	value := fmt.Sprintf("key-%08d", n) + strings.Repeat("x", 1<<20)
	_, err := outbox.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: value[:12], Version: "version", CreatedAt: time.Unix(100, 0).UTC()})
	return err
}
