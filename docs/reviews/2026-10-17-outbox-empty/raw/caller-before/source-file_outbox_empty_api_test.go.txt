package runthrough_test

import (
	"context"
	"os"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestRetryPendingRefusesEmptyJournal(t *testing.T) {
	f := newJournalRecoveryFixture(t)
	local := storage.NewMemoryStore()
	upstream := newMockUpstream()
	adapter := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, local, local, upstream, f.outbox)
	if err := os.Truncate(f.path, 0); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RetryPending(context.Background()); err == nil {
		t.Fatal("recovery reported success after the durable journal was truncated")
	}
	if calls := upstreamCalls(upstream); calls != 0 {
		t.Fatalf("corrupt journal caused %d upstream calls", calls)
	}
	data, err := os.ReadFile(f.path)
	if err != nil || len(data) != 0 {
		t.Fatalf("recovery replaced corrupt journal: bytes=%d error=%v", len(data), err)
	}
	f.assertRetainedEntries(t, f.outbox)
}
