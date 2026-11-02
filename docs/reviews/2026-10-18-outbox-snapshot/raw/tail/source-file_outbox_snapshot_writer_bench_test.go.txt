package runthrough

import (
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

type snapshotWriterResult struct {
	count uint64
	err   error
	last  []OutboxEntry
}

func benchmarkSnapshotWriter(b *testing.B, path string, fixture snapshotCostFixture, prepared bool) {
	b.Helper()
	writer, size := snapshotCostOpen(b, path, prepared)
	if err := writer.SaveUpstreamState("bucket", "target", UpstreamState{ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err != nil {
		b.Fatal(err)
	}
	observer, err := NewFileOutbox(path)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := snapshotCostRead(observer, prepared); err != nil {
		b.Fatal(err)
	}
	var stopping atomic.Bool
	ready := make(chan struct{})
	done := make(chan snapshotWriterResult, 1)
	latencies := make([]int64, b.N)
	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()
	go runSnapshotObserver(observer, prepared, &stopping, ready, done)
	<-ready
	for n := 0; n < b.N; n++ {
		value := UpstreamState{ETag: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
		if n%2 != 0 {
			value.ETag = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}
		began := time.Now()
		err := writer.SaveUpstreamState("bucket", "target", value)
		latencies[n] = time.Since(began).Nanoseconds()
		if err != nil {
			stopping.Store(true)
			<-done
			b.Fatal(err)
		}
	}
	stopping.Store(true)
	result := <-done
	b.StopTimer()
	b.ReportMetric(float64(size), "journal-B")
	b.ReportMetric(float64(outboxReadCostHWM(b)), "process-HWM-B")
	if result.err != nil || result.count == 0 {
		b.Fatalf("observer count=%d error=%v", result.count, result.err)
	}
	verifySnapshotCost(b, result.last, fixture, prepared)
	slices.Sort(latencies)
	b.ReportMetric(float64(latencies[(len(latencies)-1)/2]), "writer-p50-ns")
	b.ReportMetric(float64(latencies[(len(latencies)-1)*95/100]), "writer-p95-ns")
	b.ReportMetric(float64(latencies[len(latencies)-1]), "writer-max-ns")
	b.ReportMetric(float64(result.count), "snapshot-count")
	snapshotCostMemory(b)
	runtime.KeepAlive(result.last)
	runtime.KeepAlive(observer)
	runtime.KeepAlive(writer)
}

func runSnapshotObserver(outbox *FileOutbox, prepared bool, stopping *atomic.Bool, ready chan struct{}, done chan snapshotWriterResult) {
	var result snapshotWriterResult
	for !stopping.Load() {
		entries, err := snapshotCostRead(outbox, prepared)
		result.count++
		result.last = entries
		result.err = err
		if result.count == 1 {
			close(ready)
		}
		if err != nil {
			break
		}
	}
	done <- result
}
