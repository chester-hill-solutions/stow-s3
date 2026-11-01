package runthrough_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

type journalRecoveryFixture struct {
	outbox   *runthrough.FileOutbox
	path     string
	data     []byte
	active   runthrough.OutboxEntry
	prepared runthrough.OutboxEntry
}

func newJournalRecoveryFixture(t *testing.T) journalRecoveryFixture {
	t.Helper()
	f := journalRecoveryFixture{path: filepath.Join(t.TempDir(), "outbox")}
	var err error
	f.outbox, err = runthrough.NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.active, err = f.outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "key", Version: "local-version"})
	if err != nil {
		t.Fatal(err)
	}
	f.prepared, err = f.outbox.PrepareOwned(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "prepared"}, "original-owner", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.outbox.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{ETag: "upstream-version"}); err != nil {
		t.Fatal(err)
	}
	f.data, err = os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f journalRecoveryFixture) operation(t *testing.T, name string) error {
	t.Helper()
	switch name {
	case "open":
		_, err := runthrough.NewFileOutbox(f.path)
		return err
	case "lookup":
		_, _, err := f.outbox.UpstreamState("bucket", "key")
		return err
	case "pending":
		_, err := f.outbox.PendingSnapshot()
		return err
	case "prepared":
		_, err := f.outbox.PreparedSnapshot()
		return err
	case "enqueue":
		_, err := f.outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "new", Version: "new-version"})
		return err
	case "prepare":
		_, err := f.outbox.PrepareOwned(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "new"}, "new-owner", time.Hour)
		return err
	case "save-provenance":
		return f.outbox.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{Absent: true})
	case "close":
		return f.outbox.Close()
	default:
		t.Fatalf("unknown journal operation %q", name)
		return nil
	}
}

func (f journalRecoveryFixture) assertRetainedEntries(t *testing.T, outbox *runthrough.FileOutbox) {
	t.Helper()
	pending := outbox.Pending()
	if len(pending) != 1 || pending[0].ID != f.active.ID || pending[0].Version != f.active.Version {
		t.Fatalf("acknowledged active entry changed: %+v", pending)
	}
	prepared := outbox.Prepared()
	if len(prepared) != 1 || prepared[0].ID != f.prepared.ID || prepared[0].PreparedToken != f.prepared.PreparedToken {
		t.Fatalf("acknowledged prepared entry changed: %+v", prepared)
	}
}

func (f journalRecoveryFixture) assertRestored(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(f.path, f.data, 0o600); err != nil {
		t.Fatal(err)
	}
	f.assertRetainedEntries(t, f.outbox)
	reopened, err := runthrough.NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.assertRetainedEntries(t, reopened)
	requireLookupValue(t, reopened, runthrough.UpstreamState{ETag: "upstream-version"})
	entry, err := reopened.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "new", Version: "new-version"})
	if err != nil || entry.ID != "outbox-3" {
		t.Fatalf("resumed enqueue = %+v, error=%v", entry, err)
	}
	prepared, err := reopened.PrepareOwned(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "new-prepared"}, "next-owner", time.Hour)
	if err != nil || prepared.ID != "outbox-4" || prepared.PreparedToken <= f.prepared.PreparedToken {
		t.Fatalf("resumed prepare = %+v, error=%v", prepared, err)
	}
}

func TestFileOutboxRejectsEmptyJournal(t *testing.T) {
	for _, operation := range []string{"open", "lookup", "pending", "prepared", "enqueue", "prepare", "save-provenance", "close"} {
		t.Run(operation, func(t *testing.T) {
			f := newJournalRecoveryFixture(t)
			if err := os.Truncate(f.path, 0); err != nil {
				t.Fatal(err)
			}
			for n := 0; n < 2; n++ {
				if err := f.operation(t, operation); err == nil {
					t.Fatalf("%s accepted a journal truncated after durable enqueue %q", operation, f.active.ID)
				}
			}
			data, err := os.ReadFile(f.path)
			if err != nil || len(data) != 0 {
				t.Fatalf("failed operation replaced corrupt journal: bytes=%d error=%v", len(data), err)
			}
			f.assertRetainedEntries(t, f.outbox)
			f.assertRestored(t)
		})
	}
}

func TestFileOutboxRejectsEveryTruncatedJournal(t *testing.T) {
	f := newJournalRecoveryFixture(t)
	for cut := 0; cut < len(f.data); cut++ {
		if err := os.WriteFile(f.path, f.data[:cut], 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := runthrough.NewFileOutbox(f.path); err == nil {
			t.Fatalf("opened truncated journal at %d/%d bytes", cut, len(f.data))
		}
	}
	f.assertRestored(t)
}

func assertEmptyJournal(t *testing.T, outbox *runthrough.FileOutbox) {
	t.Helper()
	pending, err := outbox.PendingSnapshot()
	if err != nil || len(pending) != 0 {
		t.Fatalf("empty pending=%+v error=%v", pending, err)
	}
	prepared, err := outbox.PreparedSnapshot()
	if err != nil || len(prepared) != 0 {
		t.Fatalf("empty prepared=%+v error=%v", prepared, err)
	}
	value, known, err := outbox.UpstreamState("bucket", "key")
	if err != nil || known || value != (runthrough.UpstreamState{}) {
		t.Fatalf("empty provenance=%+v known=%t error=%v", value, known, err)
	}
}

func TestFileOutboxAcceptsValidEmptyJournal(t *testing.T) {
	for _, format := range []string{"missing", "protobuf", "legacy-json"} {
		t.Run(format, func(t *testing.T) {
			outbox, path := newLookupOutbox(t)
			switch format {
			case "missing":
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("new outbox unexpectedly created a journal: %v", err)
				}
			case "protobuf":
				if err := outbox.Close(); err != nil {
					t.Fatal(err)
				}
			case "legacy-json":
				if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := runthrough.NewFileOutbox(path)
			if err != nil {
				t.Fatal(err)
			}
			assertEmptyJournal(t, reopened)
		})
	}
}
