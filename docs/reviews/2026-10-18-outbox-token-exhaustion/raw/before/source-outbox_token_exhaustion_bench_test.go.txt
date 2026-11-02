package runthrough

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkOutboxFencingToken(b *testing.B) {
	for _, backend := range []string{"state", "file"} {
		for _, depth := range []int{0, 128} {
			for _, action := range []string{"claim", "prepare"} {
				b.Run(fmt.Sprintf("%s/depth=%d/%s", backend, depth, action), func(b *testing.B) { benchmarkFencingToken(b, backend, depth, action) })
			}
		}
	}
}
func benchmarkFencingToken(b *testing.B, backend string, depth int, action string) {
	b.Helper()
	storageBackend := backend
	if backend == "state" {
		storageBackend = "memory"
	}
	outbox := seedAdmissionBenchmark(b, storageBackend, depth)
	var state *outboxState
	var file *FileOutbox
	if backend == "state" {
		state = &outbox.(*MemoryOutbox).state
	} else {
		file = outbox.(*FileOutbox)
	}
	entry := OutboxEntry{ID: "fencing-probe", Operation: OutboxPut, Bucket: "bucket", Key: "probe", Version: "probe-version", CreatedAt: time.Unix(100, 0).UTC()}
	if action == "claim" {
		if _, err := outbox.Enqueue(entry); err != nil {
			b.Fatal(err)
		}
		if err := fencingBenchmarkCycle(state, file, action, entry); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if err := fencingBenchmarkCycle(state, file, action, entry); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if action == "claim" {
		if err := outbox.MarkSuccess(entry.ID); err != nil {
			b.Fatal(err)
		}
	}
	verifyAdmissionBenchmark(b, outbox, depth)
}
func fencingBenchmarkCycle(state *outboxState, file *FileOutbox, action string, entry OutboxEntry) error {
	if action == "claim" {
		var claimed ClaimedEntry
		var ok bool
		var err error
		if file == nil {
			claimed, ok, err = state.claim(entry.ID, "benchmark-owner", time.Hour)
		} else {
			claimed, ok, err = file.Claim(entry.ID, "benchmark-owner", time.Hour)
		}
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("eligible benchmark claim was not acquired")
		}
		if file == nil {
			return state.releaseClaim(entry.ID, "benchmark-owner", claimed.Entry.ClaimToken)
		}
		return file.Release(entry.ID, "benchmark-owner", claimed.Entry.ClaimToken)
	}
	var prepared OutboxEntry
	var err error
	if file == nil {
		prepared, err = state.prepareOwned(entry, "benchmark-owner", time.Hour)
	} else {
		prepared, err = file.PrepareOwned(entry, "benchmark-owner", time.Hour)
	}
	if err != nil {
		return err
	}
	if file == nil {
		return state.discardPreparedOwned(entry.ID, "benchmark-owner", prepared.PreparedToken)
	}
	return file.DiscardPreparedOwned(entry.ID, "benchmark-owner", prepared.PreparedToken)
}
