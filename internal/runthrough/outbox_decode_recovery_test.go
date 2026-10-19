package runthrough

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestFileOutboxDuplicateFloodPreservesRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox")
	outbox, err := NewFileOutbox(path)
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
	original := outbox.state.clone()
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := repeatedEmptyOutboxFrame(3, 32768)
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = outbox.PendingSnapshot()
	runtime.ReadMemStats(&after)
	if err == nil || !strings.Contains(err.Error(), "duplicate outbox") {
		t.Fatalf("invalid journal accepted: %v", err)
	}
	if after.TotalAlloc-before.TotalAlloc > 1<<20 {
		t.Error("invalid reload exceeded allocation budget")
	}
	if !reflect.DeepEqual(original, outbox.state) {
		t.Error("invalid reload changed last known state")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, invalid) {
		t.Fatal("failed reload rewrote corrupt bytes")
	}
	checkRestoredOutbox(t, outbox, valid, entry.ID)
}

func checkRestoredOutbox(t *testing.T, outbox *FileOutbox, valid []byte, id string) {
	t.Helper()
	if err := os.WriteFile(outbox.path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := outbox.PendingSnapshot()
	if err != nil || len(snapshot) != 1 || snapshot[0].ID != id {
		t.Fatalf("restored journal did not recover: %v", err)
	}
}
