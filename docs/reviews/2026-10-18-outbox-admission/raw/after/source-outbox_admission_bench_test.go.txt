package runthrough

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkOutboxAdmission(b *testing.B) {
	for _, backend := range []string{"memory", "file"} {
		for _, depth := range []int{0, 128} {
			for _, method := range []string{"enqueue", "prepare"} {
				for _, id := range []string{"auto", "explicit"} {
					b.Run(fmt.Sprintf("%s/depth=%d/%s/%s", backend, depth, method, id), func(b *testing.B) { benchmarkAdmission(b, backend, depth, method+"/"+id) })
				}
			}
		}
	}
}

func benchmarkAdmission(b *testing.B, backend string, depth int, action string) {
	outbox := seedAdmissionBenchmark(b, backend, depth)
	method, id := "enqueue", ""
	if action == "prepare/auto" || action == "prepare/explicit" {
		method = "prepare"
	}
	if action == "enqueue/explicit" || action == "prepare/explicit" {
		id = "benchmark-probe"
	}
	entry := OutboxEntry{ID: id, Operation: OutboxPut, Bucket: "bucket", Key: "probe", Version: "probe-version", CreatedAt: time.Unix(100, 0).UTC()}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		admitted, err := admitEntry(outbox, method, entry)
		if err != nil {
			b.Fatal(err)
		}
		if method == "enqueue" {
			err = outbox.MarkSuccess(admitted.ID)
		} else {
			err = outbox.DiscardPrepared(admitted.ID)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	verifyAdmissionBenchmark(b, outbox, depth)
}

func seedAdmissionBenchmark(b *testing.B, backend string, depth int) CoordinatedOutbox {
	b.Helper()
	state := newOutboxState()
	for n := 0; n < depth; n++ {
		id := fmt.Sprintf("seed-%06d", n)
		state.entries[id] = OutboxEntry{ID: id, Operation: OutboxPut, Bucket: "bucket", Key: id, Version: "retained-version", CreatedAt: time.Unix(100, 0).UTC()}
		id = "prepared-" + id
		state.prepared[id] = OutboxEntry{ID: id, Operation: OutboxPut, Bucket: "bucket", Key: id, Prepared: true, CreatedAt: time.Unix(100, 0).UTC()}
	}
	if backend == "memory" {
		return &MemoryOutbox{state: state}
	}
	outbox, err := NewFileOutbox(filepath.Join(b.TempDir(), "outbox"))
	if err != nil {
		b.Fatal(err)
	}
	if err := outbox.withState(func(s *outboxState) error { *s = state; return nil }); err != nil {
		b.Fatal(err)
	}
	return outbox
}

func verifyAdmissionBenchmark(b *testing.B, outbox CoordinatedOutbox, depth int) {
	b.Helper()
	active, prepared := outbox.Pending(), outbox.Prepared()
	if len(active) != depth || len(prepared) != depth {
		b.Fatal("admission benchmark changed retained intent counts")
	}
	for _, entry := range active {
		want := OutboxEntry{ID: entry.ID, Operation: OutboxPut, Bucket: "bucket", Key: entry.ID, Version: "retained-version", CreatedAt: time.Unix(100, 0).UTC()}
		if entry != want {
			b.Fatalf("retained active intent changed: %+v", entry)
		}
	}
	for _, entry := range prepared {
		want := OutboxEntry{ID: entry.ID, Operation: OutboxPut, Bucket: "bucket", Key: entry.ID, Prepared: true, CreatedAt: time.Unix(100, 0).UTC()}
		if entry != want {
			b.Fatalf("retained prepared intent changed: %+v", entry)
		}
	}
}
