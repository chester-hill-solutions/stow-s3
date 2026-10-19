package runthrough_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

func journalInfo(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func requireSameJournal(t *testing.T, path string, before os.FileInfo) {
	t.Helper()
	if !os.SameFile(before, journalInfo(t, path)) {
		t.Fatal("unchanged save replaced the journal")
	}
}

func TestFileOutboxUnchangedProvenanceDoesNotReplace(t *testing.T) {
	values := []runthrough.UpstreamState{{ETag: "same"}, {Absent: true}, {Unknown: true}, {}, {ETag: "same", Absent: true}, {ETag: "same", Unknown: true}}
	for n, value := range values {
		t.Run(string(rune('a'+n)), func(t *testing.T) {
			f := newJournalRecoveryFixture(t)
			if err := f.outbox.SaveUpstreamState("bucket", "key", value); err != nil {
				t.Fatal(err)
			}
			before := journalInfo(t, f.path)
			data, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := f.outbox.SaveUpstreamState("bucket", "key", value); err != nil {
					t.Fatal(err)
				}
				requireSameJournal(t, f.path, before)
				requireJournalBytes(t, f.path, data)
				requireLookupValue(t, f.outbox, value)
				f.assertRetainedEntries(t, f.outbox)
			}
		})
	}
}

func TestFileOutboxEmptyProvenanceCreatesKnownRow(t *testing.T) {
	outbox, path := newLookupOutbox(t)
	if err := outbox.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{}); err != nil {
		t.Fatal(err)
	}
	reopened, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	requireLookupValue(t, reopened, runthrough.UpstreamState{})
}

func TestFileOutboxProvenanceFlagsAreChanges(t *testing.T) {
	outbox, path := newLookupOutbox(t)
	if err := outbox.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{ETag: "same"}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []runthrough.UpstreamState{{ETag: "same", Absent: true}, {ETag: "same", Unknown: true}, {ETag: "same", Absent: true, Unknown: true}, {ETag: "different"}} {
		before := journalInfo(t, path)
		if err := outbox.SaveUpstreamState("bucket", "key", value); err != nil {
			t.Fatal(err)
		}
		if os.SameFile(before, journalInfo(t, path)) {
			t.Fatal("changed provenance did not publish a replacement")
		}
		requireLookupValue(t, outbox, value)
	}
}

func TestFileOutboxProvenanceSaveReloadsPeerValue(t *testing.T) {
	f := newJournalRecoveryFixture(t)
	peer, err := runthrough.NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{ETag: "peer"}); err != nil {
		t.Fatal(err)
	}
	want := runthrough.UpstreamState{ETag: "upstream-version"}
	if err := f.outbox.SaveUpstreamState("bucket", "key", want); err != nil {
		t.Fatal(err)
	}
	requireLookupValue(t, peer, want)
}

func TestFileOutboxEqualLegacyProvenanceStillMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.json")
	data := []byte(`{"version":3,"entries":{},"seq":42,"next_token":43,"provenance":{"bucket\u0000key":{"etag":"same"}}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	outbox, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{ETag: "same"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || len(after) < 8 || string(after[:8]) != "STOWOUT4" {
		t.Fatalf("equal-value save did not migrate legacy format: %v", err)
	}
	entry, err := outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "new", Version: "local-version"})
	if err != nil || entry.ID != "outbox-43" {
		t.Fatalf("migrated sequence = %+v, error=%v", entry, err)
	}
	claimed, ok, err := outbox.Claim(entry.ID, "owner", time.Hour)
	if err != nil || !ok || claimed.Entry.ClaimToken != 44 {
		t.Fatalf("migrated token = %+v, ok=%t error=%v", claimed, ok, err)
	}
}

func TestFileOutboxEqualSaveRepairsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX modes")
	}
	for _, mode := range []os.FileMode{0o400, 0o644, 0o700} {
		f := newJournalRecoveryFixture(t)
		if err := os.Chmod(f.path, mode); err != nil {
			t.Fatal(err)
		}
		if err := f.outbox.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{ETag: "upstream-version"}); err != nil {
			t.Fatal(err)
		}
		if got := journalInfo(t, f.path).Mode().Perm(); got != 0o600 {
			t.Fatalf("permissions=%o, want 600", got)
		}
	}
}

func TestFileOutboxEqualSaveReplacesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires unprivileged symlinks")
	}
	f := newJournalRecoveryFixture(t)
	target := f.path + ".source"
	if err := os.Rename(f.path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, f.path); err != nil {
		t.Fatal(err)
	}
	if err := f.outbox.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{ETag: "upstream-version"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(f.path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("journal is not a regular replacement: %v", err)
	}
	requireJournalBytes(t, target, f.data)
	if mode := journalInfo(t, target).Mode().Perm(); mode != 0o644 {
		t.Fatalf("symlink target permissions changed to %o", mode)
	}
}
