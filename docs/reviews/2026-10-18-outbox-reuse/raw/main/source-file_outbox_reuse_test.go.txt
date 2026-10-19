package runthrough_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

func rewriteJournalKeepingMetadata(t *testing.T, path string, data []byte) {
	t.Helper()
	before := journalInfo(t, path)
	if int64(len(data)) != before.Size() {
		t.Fatal("rewrite must preserve size")
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after := journalInfo(t, path)
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("rewrite did not preserve file identity, size and modification time")
	}
}

func TestFileOutboxReloadDetectsInPlaceEdits(t *testing.T) {
	f := newJournalRecoveryFixture(t)
	requireLookupValue(t, f.outbox, runthrough.UpstreamState{ETag: "upstream-version"})
	changed := bytes.Replace(f.data, []byte("upstream-version"), []byte("external-version"), 1)
	if bytes.Equal(changed, f.data) {
		t.Fatal("provenance replacement not installed")
	}
	rewriteJournalKeepingMetadata(t, f.path, changed)
	for n := 0; n < 2; n++ {
		requireLookupValue(t, f.outbox, runthrough.UpstreamState{ETag: "external-version"})
		f.assertRetainedEntries(t, f.outbox)
	}
	rewriteJournalKeepingMetadata(t, f.path, f.data)
	requireLookupValue(t, f.outbox, runthrough.UpstreamState{ETag: "upstream-version"})
	f.assertRetainedEntries(t, f.outbox)
}

func TestFileOutboxReloadRejectsInPlaceCorruption(t *testing.T) {
	for _, fault := range []string{"invalid-utf8", "unsupported-version"} {
		t.Run(fault, func(t *testing.T) {
			f := newJournalRecoveryFixture(t)
			requireLookupValue(t, f.outbox, runthrough.UpstreamState{ETag: "upstream-version"})
			changed := bytes.Clone(f.data)
			if fault == "invalid-utf8" {
				index := bytes.Index(changed, []byte("upstream-version"))
				if index < 0 {
					t.Fatal("provenance not present")
				}
				changed[index] = 0xff
			} else {
				if len(changed) < 14 || string(changed[:8]) != "STOWOUT4" || changed[12] != 8 || changed[13] != 4 {
					t.Fatal("unexpected protobuf frame")
				}
				changed[13] = 127
			}
			rewriteJournalKeepingMetadata(t, f.path, changed)
			for n := 0; n < 2; n++ {
				value, known, err := f.outbox.UpstreamState("bucket", "key")
				if err == nil || known || value != (runthrough.UpstreamState{}) {
					t.Fatalf("corrupt journal returned value: %+v %t %v", value, known, err)
				}
			}
			requireJournalBytes(t, f.path, changed)
			rewriteJournalKeepingMetadata(t, f.path, f.data)
			requireLookupValue(t, f.outbox, runthrough.UpstreamState{ETag: "upstream-version"})
			f.assertRetainedEntries(t, f.outbox)
		})
	}
}

func TestFileOutboxReloadAfterCommitAndHistoricalRestore(t *testing.T) {
	f := newJournalRecoveryFixture(t)
	requireLookupValue(t, f.outbox, runthrough.UpstreamState{ETag: "upstream-version"})
	if err := f.outbox.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{ETag: "external-version"}); err != nil {
		t.Fatal(err)
	}
	// Restore before another outbox read can observe the successful write.
	rewriteJournalKeepingMetadata(t, f.path, f.data)
	requireLookupValue(t, f.outbox, runthrough.UpstreamState{ETag: "upstream-version"})
	f.assertRetainedEntries(t, f.outbox)
	next, err := f.outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "new", Version: "new-version"})
	if err != nil || next.ID != "outbox-3" {
		t.Fatalf("restored sequence: %+v %v", next, err)
	}
}
