package runthrough

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func BenchmarkFileOutboxSnapshotMatchedWriter(b *testing.B) {
	root := os.Getenv("STOW_OUTBOX_SNAPSHOT_FIXTURES")
	if root == "" {
		b.Skip("requires separately seeded snapshot fixtures")
	}
	for _, fixture := range snapshotCostFixtures() {
		if fixture.name != "active=16384" && fixture.name != "prepared=16384" {
			continue
		}
		stage := "writer-pending"
		prepared := fixture.name == "prepared=16384"
		if prepared {
			stage = "writer-prepared"
		}
		b.Run(fixture.name, func(b *testing.B) {
			b.Run(stage, func(b *testing.B) {
				benchmarkMatchedSnapshotWriter(b, filepath.Join(root, fixture.name), fixture, prepared)
			})
		})
	}
}

func benchmarkMatchedSnapshotWriter(b *testing.B, path string, fixture snapshotCostFixture, prepared bool) {
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
	requests := make(chan struct{})
	responses := make(chan snapshotWriterResult)
	latencies := make([]int64, b.N)
	var last snapshotWriterResult
	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()
	go runMatchedSnapshotObserver(observer, prepared, requests, responses)
	for n := 0; n < b.N; n++ {
		value := UpstreamState{ETag: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
		if n%2 != 0 {
			value.ETag = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}
		requests <- struct{}{}
		began := time.Now()
		err := writer.SaveUpstreamState("bucket", "target", value)
		latencies[n] = time.Since(began).Nanoseconds()
		last = <-responses
		if err != nil || last.err != nil {
			close(requests)
			for range responses {
			}
			b.Fatalf("writer=%v snapshot=%v", err, last.err)
		}
	}
	close(requests)
	for range responses {
	}
	b.StopTimer()
	b.ReportMetric(float64(size), "journal-B")
	b.ReportMetric(float64(outboxReadCostHWM(b)), "process-HWM-B")
	verifySnapshotCost(b, last.last, fixture, prepared)
	slices.Sort(latencies)
	b.ReportMetric(float64(latencies[(len(latencies)-1)/2]), "writer-p50-ns")
	b.ReportMetric(float64(latencies[(len(latencies)-1)*95/100]), "writer-p95-ns")
	b.ReportMetric(float64(latencies[len(latencies)-1]), "writer-max-ns")
	b.ReportMetric(float64(b.N), "snapshot-count")
	snapshotCostMemory(b)
	runtime.KeepAlive(last.last)
	runtime.KeepAlive(observer)
	runtime.KeepAlive(writer)
}

func runMatchedSnapshotObserver(outbox *FileOutbox, prepared bool, requests <-chan struct{}, responses chan<- snapshotWriterResult) {
	defer close(responses)
	for range requests {
		entries, err := snapshotCostRead(outbox, prepared)
		responses <- snapshotWriterResult{count: 1, last: entries, err: err}
	}
}
