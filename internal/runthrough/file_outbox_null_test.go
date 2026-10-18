package runthrough_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func requireJournalBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("rejected input was replaced: got=%q want=%q error=%v", data, want, err)
	}
}

func TestFileOutboxRefusesNullJournal(t *testing.T) {
	for _, input := range []struct{ name, payload string }{{"plain", "null"}, {"whitespace", " \nnull\t"}} {
		t.Run(input.name, func(t *testing.T) {
			for _, operation := range []string{"open", "lookup", "pending", "prepared", "enqueue", "prepare", "save-provenance", "close"} {
				t.Run(operation, func(t *testing.T) {
					f := newJournalRecoveryFixture(t)
					payload := []byte(input.payload)
					if err := os.WriteFile(f.path, payload, 0o600); err != nil {
						t.Fatal(err)
					}
					for n := 0; n < 2; n++ {
						if err := f.operation(t, operation); err == nil {
							t.Fatalf("%s accepted JSON null as a snapshot after acknowledged enqueue %q", operation, f.active.ID)
						}
					}
					requireJournalBytes(t, f.path, payload)
					f.assertRetainedEntries(t, f.outbox)
					f.assertRestored(t)
				})
			}
		})
	}
}

func TestFileOutboxRefusesNonObjectJournalRoots(t *testing.T) {
	for _, payload := range []string{"true", "123", "\"string\"", "[]"} {
		t.Run(payload, func(t *testing.T) {
			for _, operation := range []string{"open", "lookup", "pending", "prepared", "enqueue", "prepare", "save-provenance", "close"} {
				t.Run(operation, func(t *testing.T) {
					f := newJournalRecoveryFixture(t)
					if err := os.WriteFile(f.path, []byte(payload), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := f.operation(t, operation); err == nil {
						t.Fatalf("%s accepted non-object journal root %q", operation, payload)
					}
					requireJournalBytes(t, f.path, []byte(payload))
					f.assertRetainedEntries(t, f.outbox)
				})
			}
		})
	}
}

func TestRetryPendingRefusesNullJournal(t *testing.T) {
	f := newJournalRecoveryFixture(t)
	local := storage.NewMemoryStore()
	upstream := newMockUpstream()
	adapter := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, local, local, upstream, f.outbox)
	payload := []byte("null")
	if err := os.WriteFile(f.path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RetryPending(context.Background()); err == nil {
		t.Fatal("recovery reported success after the journal was replaced with JSON null")
	}
	if calls := upstreamCalls(upstream); calls != 0 {
		t.Fatalf("null journal caused %d upstream calls", calls)
	}
	requireJournalBytes(t, f.path, payload)
	f.assertRetainedEntries(t, f.outbox)
	f.assertRestored(t)
}
