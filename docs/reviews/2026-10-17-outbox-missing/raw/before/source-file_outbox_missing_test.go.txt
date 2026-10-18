package runthrough_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func requireMissingJournal(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing journal error = %v, want os.ErrNotExist", err)
	}
}

func TestFileOutboxRefusesMissingLiveJournal(t *testing.T) {
	for _, view := range []string{"writer", "reopened"} {
		t.Run(view, func(t *testing.T) {
			for _, operation := range []string{"lookup", "pending", "prepared", "enqueue", "prepare", "save-provenance", "close"} {
				t.Run(operation, func(t *testing.T) {
					f := newJournalRecoveryFixture(t)
					if view == "reopened" {
						var err error
						f.outbox, err = runthrough.NewFileOutbox(f.path)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Remove(f.path); err != nil {
						t.Fatal(err)
					}
					for n := 0; n < 2; n++ {
						requireMissingJournal(t, f.operation(t, operation))
					}
					if _, err := os.Stat(f.path); !os.IsNotExist(err) {
						t.Fatalf("failed operation recreated missing journal: %v", err)
					}
					f.assertRetainedEntries(t, f.outbox)
					f.assertRestored(t)
				})
			}
		})
	}
}

func TestFileOutboxTracksFirstEnqueue(t *testing.T) {
	outbox, path := newLookupOutbox(t)
	if _, err := outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "key", Version: "local-version"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	requireMissingJournal(t, outbox.Close())
}

func TestFileOutboxLearnsPeerJournal(t *testing.T) {
	observer, path := newLookupOutbox(t)
	writer, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := writer.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "key", Version: "local-version"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := observer.PendingSnapshot()
	if err != nil || len(pending) != 1 || pending[0].ID != entry.ID {
		t.Fatalf("peer snapshot = %+v, error=%v", pending, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err = observer.PendingSnapshot()
	requireMissingJournal(t, err)
}

func TestFileOutboxTracksEmptyJournal(t *testing.T) {
	writer, path := newLookupOutbox(t)
	observer, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	assertEmptyJournal(t, writer)
	assertEmptyJournal(t, observer)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	assertEmptyJournal(t, observer)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, outbox := range []*runthrough.FileOutbox{writer, observer} {
		_, err := outbox.PendingSnapshot()
		requireMissingJournal(t, err)
	}
}

func TestFileOutboxFreshWriteFailureCanRetry(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged POSIX directory permissions")
	}
	outbox, path := newLookupOutbox(t)
	dir := filepath.Dir(path)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	entry := runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "key", Version: "local-version"}
	if _, err := outbox.Enqueue(entry); err == nil {
		t.Fatal("first write unexpectedly succeeded without directory write permission")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed first write published a journal: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	written, err := outbox.Enqueue(entry)
	if err != nil || written.ID != "outbox-1" {
		t.Fatalf("retry after unacknowledged first write = %+v, error=%v", written, err)
	}
}

func TestRetryPendingRefusesMissingJournal(t *testing.T) {
	f := newJournalRecoveryFixture(t)
	local := storage.NewMemoryStore()
	upstream := newMockUpstream()
	adapter := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, local, local, upstream, f.outbox)
	if err := os.Remove(f.path); err != nil {
		t.Fatal(err)
	}
	requireMissingJournal(t, adapter.RetryPending(context.Background()))
	if calls := upstreamCalls(upstream); calls != 0 {
		t.Fatalf("missing journal caused %d upstream calls", calls)
	}
	f.assertRetainedEntries(t, f.outbox)
	f.assertRestored(t)
}
