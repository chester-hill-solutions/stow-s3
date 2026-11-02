package runthrough

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type bindingSnapshots struct{ pending, prepared []OutboxEntry }

func bindingFormats() []string { return append(migrationFormats(), "flat") }

func bindingState(collection, kind string) outboxState {
	state := newOutboxState()
	state.seq, state.nextToken = 2, 7
	stamp := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	first := OutboxEntry{ID: "outbox-1", Operation: OutboxPut, Bucket: "bucket", Key: "first", Version: "first-version", CreatedAt: stamp, Attempts: 2, Prepared: collection == "prepared"}
	second := OutboxEntry{ID: "outbox-2", Operation: OutboxPut, Bucket: "bucket", Key: "second", Version: "second-version", CreatedAt: stamp.Add(time.Second), Prepared: collection == "prepared"}
	switch kind {
	case "missing":
		first.ID = ""
	case "dangling":
		first.ID = "unmapped-record"
	case "aliased":
		first.ID = second.ID
	}
	entries := state.entries
	if collection == "prepared" {
		entries = state.prepared
	}
	entries["outbox-1"], entries["outbox-2"] = first, second
	return state
}

func encodeBindingState(t *testing.T, format string, state outboxState) []byte {
	t.Helper()
	if format == "flat" {
		state = state.clone()
		for id, entry := range state.prepared {
			state.entries[id] = entry
		}
	}
	return encodeMigrationState(t, format, state)
}

func bindingSnapshot(outbox *FileOutbox, collection string) ([]OutboxEntry, error) {
	if collection == "prepared" {
		return outbox.PreparedSnapshot()
	}
	return outbox.PendingSnapshot()
}

func finishBindingEntry(outbox *FileOutbox, collection, id string) (OutboxEntry, error) {
	if collection == "prepared" {
		return outbox.Commit(id, "first-version")
	}
	return OutboxEntry{}, outbox.MarkSuccess(id)
}

func TestFileOutboxRejectsMismatchedEntryIDsOnOpen(t *testing.T) {
	for _, format := range bindingFormats() {
		for _, collection := range []string{"entries", "prepared"} {
			for _, kind := range []string{"missing", "dangling", "aliased"} {
				t.Run(format+"/"+collection+"/"+kind, func(t *testing.T) {
					data := encodeBindingState(t, format, bindingState(collection, kind))
					path := filepath.Join(t.TempDir(), "outbox")
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					outbox, err := NewFileOutbox(path)
					if err == nil {
						demonstrateBindingAcceptance(t, outbox, path, data, collection)
					}
					if !strings.Contains(err.Error(), "does not match") {
						t.Fatalf("wrong rejection: %v", err)
					}
					requireMigrationBytes(t, path, data)
				})
			}
		}
	}
}

func demonstrateBindingAcceptance(t *testing.T, outbox *FileOutbox, path string, original []byte, collection string) {
	t.Helper()
	entries, err := bindingSnapshot(outbox, collection)
	if err != nil || len(entries) != 2 || entries[0].Key != "first" {
		t.Fatalf("invalid reproducer: %+v %v", entries, err)
	}
	committed, finishErr := finishBindingEntry(outbox, collection, entries[0].ID)
	remaining, snapshotErr := bindingSnapshot(outbox, collection)
	after, readErr := os.ReadFile(path)
	wrongRecord := entries[0].ID == "outbox-2" && finishErr == nil && len(remaining) == 1 && remaining[0].Key == "first" && (collection != "prepared" || committed.Key == "second")
	t.Fatalf("accepted mismatched identity: advertised=%q wrong-record=%t committed=%+v remaining=%+v finish=%v snapshot=%v rewritten=%t read=%v", entries[0].ID, wrongRecord, committed, remaining, finishErr, snapshotErr, !bytes.Equal(original, after), readErr)
}

func TestFileOutboxRejectsMismatchedEntryIDsOnReload(t *testing.T) {
	for _, format := range bindingFormats() {
		for _, collection := range []string{"entries", "prepared"} {
			for _, operation := range []string{"snapshot", "finish", "close"} {
				t.Run(format+"/"+collection+"/"+operation, func(t *testing.T) { checkBindingReload(t, format, collection, operation) })
			}
		}
	}
}

func checkBindingReload(t *testing.T, format, collection, operation string) {
	t.Helper()
	original := encodeBindingState(t, format, bindingState(collection, ""))
	bad := encodeBindingState(t, format, bindingState(collection, "aliased"))
	path := filepath.Join(t.TempDir(), "outbox")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	want := bindingSnapshots{pending: outbox.Pending(), prepared: outbox.Prepared()}
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	switch operation {
	case "snapshot":
		_, err = bindingSnapshot(outbox, collection)
	case "finish":
		_, err = finishBindingEntry(outbox, collection, "outbox-2")
	default:
		err = outbox.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("%s accepted mismatched IDs: %v", operation, err)
	}
	requireMigrationBytes(t, path, bad)
	if !reflect.DeepEqual(outbox.Pending(), want.pending) || !reflect.DeepEqual(outbox.Prepared(), want.prepared) {
		t.Error("last acknowledged snapshots changed")
	}
	checkBindingRepair(t, outbox, path, original, want)
}

func checkBindingRepair(t *testing.T, outbox *FileOutbox, path string, original []byte, want bindingSnapshots) {
	t.Helper()
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.PendingSnapshot()
	if err != nil || !reflect.DeepEqual(pending, want.pending) {
		t.Fatalf("restored pending: %+v %v", pending, err)
	}
	prepared, err := outbox.PreparedSnapshot()
	if err != nil || !reflect.DeepEqual(prepared, want.prepared) {
		t.Fatalf("restored prepared: %+v %v", prepared, err)
	}
	entry, err := outbox.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "after-repair", Version: "new"})
	if err != nil || entry.ID != "outbox-3" {
		t.Fatalf("recovery sequence: %+v %v", entry, err)
	}
}

func TestFileOutboxPreservesMatchingCustomEntryIDs(t *testing.T) {
	for _, format := range bindingFormats() {
		for _, collection := range []string{"entries", "prepared"} {
			t.Run(format+"/"+collection, func(t *testing.T) { checkMatchingBinding(t, format, collection) })
		}
	}
}

func checkMatchingBinding(t *testing.T, format, collection string) {
	t.Helper()
	state := bindingState(collection, "")
	entries := state.entries
	if collection == "prepared" {
		entries = state.prepared
	}
	first, second := entries["outbox-1"], entries["outbox-2"]
	delete(entries, "outbox-1")
	delete(entries, "outbox-2")
	first.ID, second.ID = "custom/雪\x00id", "outbox-9"
	entries[first.ID], entries[second.ID] = first, second
	path := filepath.Join(t.TempDir(), "outbox")
	data := encodeBindingState(t, format, state)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Attempted = true
	got, err := bindingSnapshot(outbox, collection)
	if err != nil || !reflect.DeepEqual(got, []OutboxEntry{first, second}) {
		t.Fatalf("matching IDs changed: %+v %v", got, err)
	}
	requireMigrationBytes(t, path, data)
	finishMatchingBinding(t, outbox, collection, first)
	if err := outbox.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = bindingSnapshot(reopened, collection)
	if err != nil || !reflect.DeepEqual(got, []OutboxEntry{second}) {
		t.Fatalf("unprocessed record changed: %+v %v", got, err)
	}
	next, err := reopened.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "next", Version: "next-version"})
	if err != nil || next.ID != "outbox-10" {
		t.Fatalf("matching ID sequence: %+v %v", next, err)
	}
}

func finishMatchingBinding(t *testing.T, outbox *FileOutbox, collection string, first OutboxEntry) {
	t.Helper()
	committed, err := finishBindingEntry(outbox, collection, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if collection == "prepared" {
		if committed.ID != first.ID || committed.Key != first.Key || committed.Version != "first-version" {
			t.Fatalf("wrong prepared committed: %+v", committed)
		}
		if err := outbox.MarkSuccess(first.ID); err != nil {
			t.Fatal(err)
		}
	}
}
