package runthrough

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func migrationCollisionState(identical bool) outboxState {
	stamp := time.Date(2040, 1, 2, 3, 4, 5, 0, time.UTC)
	state := newOutboxState()
	state.seq, state.nextToken = 100, 1000
	state.provenance[outboxIdentity("bucket", "target")] = UpstreamState{ETag: "observed-upstream"}
	first := OutboxEntry{ID: "outbox-7", Operation: OutboxPut, Bucket: "bucket", Key: "first-local-write", Version: "first-version", PreviousVersion: "previous-first", Prepared: true, PreparedOwner: "first-owner", PreparedUntil: stamp, PreparedToken: 41, CreatedAt: stamp}
	second := first
	second.Key = "second-local-write"
	second.Version = "second-version"
	second.PreviousVersion = "previous-second"
	second.PreparedOwner = "second-owner"
	second.PreparedToken = 42
	if identical {
		first = second
	}
	state.entries[first.ID] = first
	state.prepared[second.ID] = second
	return state
}

func encodeMigrationState(t *testing.T, format string, state outboxState) []byte {
	t.Helper()
	var data []byte
	var err error
	switch format {
	case "protobuf":
		data, err = encodeOutboxProto(state)
	case "flat":
		data, err = json.Marshal(state.entries)
	default:
		var version int
		if _, err := fmt.Sscanf(format, "json-%d", &version); err != nil {
			t.Fatal(err)
		}
		data, err = json.Marshal(persistedOutbox{Version: version, Provenance: state.provenance, Entries: state.entries, Prepared: state.prepared, Seq: state.seq, NextToken: state.nextToken})
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func migrationFormats() []string { return []string{"json-0", "json-1", "json-2", "json-3", "protobuf"} }

func requireMigrationBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("journal changed: bytes=%d want=%d error=%v", len(got), len(want), err)
	}
}

func TestOutboxMigrationRejectsOverlappingPreparedIDs(t *testing.T) {
	for _, marked := range []bool{false, true} {
		for _, identical := range []bool{false, true} {
			t.Run(fmt.Sprintf("marked=%t/identical=%t", marked, identical), func(t *testing.T) {
				state := migrationCollisionState(identical)
				entry := state.entries["outbox-7"]
				entry.Prepared = marked
				state.entries[entry.ID] = entry
				before := state.clone()
				_, err := normalizeDecodedOutboxState(state)
				if err == nil || !strings.Contains(err.Error(), "both prepared and active") {
					t.Errorf("overlapping IDs accepted: error=%v", err)
				}
				if !reflect.DeepEqual(state, before) {
					t.Errorf("rejection/migration changed input records: before=%+v after=%+v", before, state)
				}
			})
		}
	}
}

func TestFileOutboxRefusesCollidingPreparedMigrationOnOpen(t *testing.T) {
	for _, format := range migrationFormats() {
		for _, identical := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/identical=%t", format, identical), func(t *testing.T) {
				state := migrationCollisionState(identical)
				data := encodeMigrationState(t, format, state)
				path := filepath.Join(t.TempDir(), "outbox")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				outbox, err := NewFileOutbox(path)
				if err == nil {
					surviving, snapshotErr := outbox.PreparedSnapshot()
					closeErr := outbox.Close()
					after, readErr := os.ReadFile(path)
					t.Fatalf("opened overlapping journal: surviving=%+v snapshot=%v close=%v rewritten=%t read=%v", surviving, snapshotErr, closeErr, !bytes.Equal(data, after), readErr)
				}
				requireMigrationBytes(t, path, data)
			})
		}
	}
}

func migrationOperation(outbox *FileOutbox, operation string) error {
	switch operation {
	case "pending":
		_, err := outbox.PendingSnapshot()
		return err
	case "prepared":
		_, err := outbox.PreparedSnapshot()
		return err
	case "lookup":
		_, _, err := outbox.UpstreamState("bucket", "target")
		return err
	case "enqueue":
		_, err := outbox.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "new-local", Version: "new-version"})
		return err
	case "prepare":
		_, err := outbox.Prepare(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "new-prepared"})
		return err
	case "save":
		return outbox.SaveUpstreamState("bucket", "target", UpstreamState{ETag: "changed-upstream"})
	default:
		return outbox.Close()
	}
}

func TestFileOutboxRefusesCollidingPreparedMigrationOnReload(t *testing.T) {
	for _, format := range migrationFormats() {
		for _, operation := range []string{"pending", "prepared", "lookup", "enqueue", "prepare", "save", "close"} {
			t.Run(format+"/"+operation, func(t *testing.T) { checkMigrationReload(t, format, operation) })
		}
	}
}

func checkMigrationReload(t *testing.T, format, operation string) {
	t.Helper()
	collision := migrationCollisionState(false)
	healthy := collision.clone()
	healthy.entries = make(map[string]OutboxEntry)
	original := encodeMigrationState(t, format, healthy)
	bad := encodeMigrationState(t, format, collision)
	path := filepath.Join(t.TempDir(), "outbox")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []OutboxEntry{healthy.prepared["outbox-7"]}
	got, err := outbox.PreparedSnapshot()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("healthy snapshot=%+v error=%v", got, err)
	}
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrationOperation(outbox, operation); err == nil {
		t.Errorf("%s accepted overlapping journal", operation)
	}
	requireMigrationBytes(t, path, bad)
	if got := outbox.Prepared(); !reflect.DeepEqual(got, want) {
		t.Errorf("last acknowledged prepared record changed: %+v", got)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = outbox.PreparedSnapshot()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("restored snapshot=%+v error=%v", got, err)
	}
	checkMigrationRecoveryCounters(t, outbox)
}

func checkMigrationRecoveryCounters(t *testing.T, outbox *FileOutbox) {
	t.Helper()
	entry, err := outbox.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "recovery", Version: "recovery-version"})
	if err != nil || entry.ID != "outbox-101" {
		t.Fatalf("recovery sequence=%+v error=%v", entry, err)
	}
	prepared, err := outbox.PrepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "new-recovery"}, "recovery-owner", time.Hour)
	if err != nil || prepared.ID != "outbox-102" || prepared.PreparedToken != 1001 {
		t.Fatalf("recovery fencing=%+v error=%v", prepared, err)
	}
}

func TestOutboxMigrationPreservesNonOverlappingRecords(t *testing.T) {
	for _, format := range append(migrationFormats(), "flat") {
		t.Run(format, func(t *testing.T) { checkNonOverlappingMigration(t, format) })
	}
}

func checkNonOverlappingMigration(t *testing.T, format string) {
	t.Helper()
	state := migrationCollisionState(false)
	second := state.prepared["outbox-7"]
	delete(state.prepared, "outbox-7")
	second.ID = "outbox-8"
	state.prepared[second.ID] = second
	active := state.entries["outbox-7"]
	active.ID, active.Key, active.Prepared = "outbox-6", "active-local", false
	state.entries[active.ID] = active
	if format == "flat" {
		state.prepared = make(map[string]OutboxEntry)
		state.seq, state.nextToken = 0, 0
		state.provenance = make(map[string]UpstreamState)
	}
	data := encodeMigrationState(t, format, state)
	path := filepath.Join(t.TempDir(), "outbox")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.PendingSnapshot()
	if err != nil || !reflect.DeepEqual(pending, []OutboxEntry{active}) {
		t.Fatalf("pending=%+v error=%v", pending, err)
	}
	wantPrepared := []OutboxEntry{state.entries["outbox-7"]}
	if format != "flat" {
		wantPrepared = append(wantPrepared, second)
	}
	staged, err := outbox.PreparedSnapshot()
	if err != nil || !reflect.DeepEqual(staged, wantPrepared) {
		t.Fatalf("prepared=%+v error=%v want=%+v", staged, err, wantPrepared)
	}
	requireMigrationBytes(t, path, data)
	if err := outbox.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	staged, err = reopened.PreparedSnapshot()
	if err != nil || !reflect.DeepEqual(staged, wantPrepared) {
		t.Fatalf("reopened prepared=%+v error=%v", staged, err)
	}
}
