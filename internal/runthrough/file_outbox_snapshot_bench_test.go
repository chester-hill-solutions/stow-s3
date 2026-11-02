package runthrough

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type snapshotCostFixture struct {
	name                         string
	provenance, active, prepared int
}

func snapshotCostFixtures() []snapshotCostFixture {
	return []snapshotCostFixture{
		{"empty", 0, 0, 0}, {"small", 2048, 128, 128},
		{"provenance=16384", 16384, 1, 1}, {"provenance=65536", 65536, 1, 1}, {"provenance=262144", 262144, 1, 1},
		{"active=16384", 0, 16384, 1}, {"prepared=16384", 0, 1, 16384}, {"mixed=2048", 16384, 2048, 2048},
	}
}

func TestOutboxSnapshotSeed(t *testing.T) {
	root := os.Getenv("STOW_OUTBOX_SNAPSHOT_SEED")
	if root == "" {
		t.Skip("separate-process snapshot benchmark fixture generator")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{}
	for _, fixture := range snapshotCostFixtures() {
		path := filepath.Join(root, fixture.name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("fixture must be new: %s %v", path, err)
		}
		outbox, err := NewFileOutbox(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := outbox.withState(func(state *outboxState) error { seedSnapshotState(state, fixture); return nil }); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest[fixture.name] = map[string]any{"provenanceRows": fixture.provenance + 1, "pendingRows": fixture.active, "preparedRows": fixture.prepared, "bytes": info.Size()}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func seedSnapshotState(state *outboxState, fixture snapshotCostFixture) {
	stamp := time.Unix(1700000000, 0).UTC()
	for n := 0; n < fixture.provenance; n++ {
		state.provenance[outboxIdentity("bucket", fmt.Sprintf("seed-%06d", n))] = UpstreamState{ETag: "0123456789abcdef0123456789abcdef"}
	}
	state.provenance[outboxIdentity("bucket", "target")] = UpstreamState{ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for n := 0; n < fixture.active; n++ {
		state.enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: fmt.Sprintf("active-%06d", n), Version: "committed-version", UpstreamAbsent: true, CreatedAt: stamp})
	}
	for n := 0; n < fixture.prepared; n++ {
		state.prepare(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: fmt.Sprintf("prepared-%06d", n), UpstreamAbsent: true, CreatedAt: stamp})
	}
}

func BenchmarkFileOutboxSnapshot(b *testing.B) {
	root := os.Getenv("STOW_OUTBOX_SNAPSHOT_FIXTURES")
	if root == "" {
		b.Skip("requires separately seeded snapshot fixtures")
	}
	for _, fixture := range snapshotCostFixtures() {
		b.Run(fixture.name, func(b *testing.B) {
			for _, stage := range []string{"pending", "prepared", "writer-pending", "writer-prepared"} {
				b.Run(stage, func(b *testing.B) {
					path := filepath.Join(root, fixture.name)
					if stage == "writer-pending" || stage == "writer-prepared" {
						benchmarkSnapshotWriter(b, path, fixture, stage == "writer-prepared")
						return
					}
					benchmarkSnapshotRead(b, path, fixture, stage == "prepared")
				})
			}
		})
	}
}

func snapshotCostRead(outbox *FileOutbox, prepared bool) ([]OutboxEntry, error) {
	if prepared {
		return outbox.PreparedSnapshot()
	}
	return outbox.PendingSnapshot()
}

func snapshotCostOpen(b *testing.B, path string, prepared bool) (*FileOutbox, int64) {
	b.Helper()
	outbox, err := NewFileOutbox(path)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := snapshotCostRead(outbox, prepared); err != nil {
		b.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	return outbox, info.Size()
}

func verifySnapshotCost(b *testing.B, entries []OutboxEntry, fixture snapshotCostFixture, prepared bool) {
	b.Helper()
	count, first := fixture.active, uint64(1)
	if prepared {
		count, first = fixture.prepared, uint64(fixture.active+1)
	}
	if entries == nil || len(entries) != count {
		b.Fatalf("snapshot size=%d nil=%t, want %d", len(entries), entries == nil, count)
	}
	for n, entry := range entries {
		if entry.ID != fmt.Sprintf("outbox-%d", first+uint64(n)) || entry.Prepared != prepared {
			b.Fatalf("snapshot row %d: %+v", n, entry)
		}
	}
}

func snapshotCostMemory(b *testing.B) {
	b.Helper()
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	b.ReportMetric(float64(memory.HeapAlloc), "process-live-heap-B")
}

func benchmarkSnapshotRead(b *testing.B, path string, fixture snapshotCostFixture, prepared bool) {
	b.Helper()
	outbox, size := snapshotCostOpen(b, path, prepared)
	var entries []OutboxEntry
	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		next, err := snapshotCostRead(outbox, prepared)
		runtime.KeepAlive(entries)
		entries = next
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(size), "journal-B")
	b.ReportMetric(float64(outboxReadCostHWM(b)), "process-HWM-B")
	verifySnapshotCost(b, entries, fixture, prepared)
	snapshotCostMemory(b)
	runtime.KeepAlive(entries)
	runtime.KeepAlive(outbox)
}
