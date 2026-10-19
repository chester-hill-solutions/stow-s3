package runthrough_test

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

func snapshotFixture(t *testing.T) (*runthrough.FileOutbox, string, []runthrough.OutboxEntry, []runthrough.OutboxEntry) {
	t.Helper()
	outbox, path := newLookupOutbox(t)
	stamp := time.Unix(1700000000, 0).UTC()
	base := runthrough.OutboxEntry{Operation: runthrough.OutboxCopy, Bucket: "bucket", SourceBucket: "source", SourceKey: "original", Version: "local-version", PreviousVersion: "previous-version", UpstreamVersion: "upstream-version", UpstreamAbsent: true, CreatedAt: stamp, Attempts: 2, NextAttempt: stamp.Add(time.Hour), LastError: "retained-error", Terminal: true, ClaimUntil: stamp.Add(time.Minute), ClaimToken: 41, Attempted: true, PreparedUntil: stamp.Add(2 * time.Hour), PreparedToken: 42}
	active := map[string]runthrough.OutboxEntry{}
	for _, id := range []string{"outbox-11", "outbox-2", "outbox-3"} {
		value := base
		value.ID, value.Key = id, id
		if id == "outbox-3" {
			value.CreatedAt = stamp.Add(-time.Second)
		}
		got, err := outbox.Enqueue(value)
		if err != nil {
			t.Fatal(err)
		}
		active[id] = got
	}
	prepared := map[string]runthrough.OutboxEntry{}
	for _, id := range []string{"outbox-15", "outbox-13"} {
		value := base
		value.ID, value.Key = id, id
		got, err := outbox.PrepareOwned(value, "snapshot-owner", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		prepared[id] = got
	}
	return outbox, path, []runthrough.OutboxEntry{active["outbox-3"], active["outbox-2"], active["outbox-11"]}, []runthrough.OutboxEntry{prepared["outbox-13"], prepared["outbox-15"]}
}

func readSnapshot(outbox *runthrough.FileOutbox, prepared bool) ([]runthrough.OutboxEntry, error) {
	if prepared {
		return outbox.PreparedSnapshot()
	}
	return outbox.PendingSnapshot()
}

func writeSnapshotEntry(outbox *runthrough.FileOutbox, prepared bool, value runthrough.OutboxEntry) (runthrough.OutboxEntry, error) {
	value.ID = ""
	if prepared {
		return outbox.PrepareOwned(value, "snapshot-owner", time.Hour)
	}
	return outbox.Enqueue(value)
}

func requireSnapshot(t *testing.T, outbox *runthrough.FileOutbox, prepared bool, want []runthrough.OutboxEntry) []runthrough.OutboxEntry {
	t.Helper()
	got, err := readSnapshot(outbox, prepared)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot=%+v error=%v, want %+v", got, err, want)
	}
	return got
}

func TestFileOutboxSnapshotsOwnValuesAndPreserveOrder(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) { checkSnapshotOwnership(t, prepared) })
	}
}

func checkSnapshotOwnership(t *testing.T, prepared bool) {
	t.Helper()
	outbox, path, active, staged := snapshotFixture(t)
	want := active
	if prepared {
		want = staged
	}
	got := requireSnapshot(t, outbox, prepared, want)
	got[0].ID = "caller-id"
	got[0].Version = "caller-version"
	got[0].ClaimToken = 999
	got[0].CreatedAt = time.Time{}
	requireSnapshot(t, outbox, prepared, want)
	held := requireSnapshot(t, outbox, prepared, want)
	peer, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := want[0]
	changed.Key = "snapshot-peer-append"
	changed.Version = "peer-version"
	changed.CreatedAt = changed.CreatedAt.Add(24 * time.Hour)
	changed, err = writeSnapshotEntry(peer, prepared, changed)
	if err != nil {
		t.Fatal(err)
	}
	updated := append(slices.Clone(want), changed)
	requireSnapshot(t, outbox, prepared, updated)
	if !reflect.DeepEqual(held, want) {
		t.Fatalf("held snapshot changed: %+v", held)
	}
}

func TestFileOutboxSnapshotErrorsReturnNoStaleValues(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) {
			outbox, path, active, staged := snapshotFixture(t)
			want := active
			if prepared {
				want = staged
			}
			requireSnapshot(t, outbox, prepared, want)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readSnapshot(outbox, prepared)
			if err == nil || got != nil {
				t.Fatalf("failed snapshot returned %+v, error=%v", got, err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			requireSnapshot(t, outbox, prepared, want)
		})
	}
}

func TestFileOutboxEmptySnapshotsRemainNonNil(t *testing.T) {
	outbox, _ := newLookupOutbox(t)
	for n := 0; n < 2; n++ {
		for _, prepared := range []bool{false, true} {
			got, err := readSnapshot(outbox, prepared)
			if err != nil || got == nil || len(got) != 0 {
				t.Fatalf("empty snapshot=%+v error=%v", got, err)
			}
		}
		if err := outbox.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileOutboxConcurrentSnapshotsAndWriter(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) { checkConcurrentSnapshots(t, prepared) })
	}
}

func checkConcurrentSnapshots(t *testing.T, prepared bool) {
	t.Helper()
	first, path, active, staged := snapshotFixture(t)
	want := active
	if prepared {
		want = staged
	}
	peer, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := want[0]
	entry.Key = "snapshot-writer-0"
	entry.CreatedAt = entry.CreatedAt.Add(-time.Hour)
	entry.Version, entry.LastError = "one", "one"
	if _, err := writeSnapshotEntry(peer, prepared, entry); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	start := make(chan struct{})
	failures := make(chan error, 3)
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for n := 0; n < 16; n++ {
			value := entry
			value.Key = fmt.Sprintf("snapshot-writer-%d", n+1)
			if n%2 == 0 {
				value.Version, value.LastError = "two", "two"
			}
			if _, err := writeSnapshotEntry(peer, prepared, value); err != nil {
				failures <- err
				return
			}
		}
	}()
	for _, reader := range []*runthrough.FileOutbox{first, peer} {
		workers.Add(1)
		go func(reader *runthrough.FileOutbox) {
			defer workers.Done()
			<-start
			if err := readConcurrentSnapshots(reader, prepared, len(want)+1); err != nil {
				failures <- err
			}
		}(reader)
	}
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func readConcurrentSnapshots(outbox *runthrough.FileOutbox, prepared bool, count int) error {
	maximum := count + 16
	for n := 0; n < 32; n++ {
		got, err := readSnapshot(outbox, prepared)
		if err != nil || len(got) < count || len(got) > maximum {
			return fmt.Errorf("snapshot count=%d error=%v", len(got), err)
		}
		count = len(got)
		for _, entry := range got {
			if strings.HasPrefix(entry.Key, "snapshot-writer-") && ((entry.Version != "one" && entry.Version != "two") || entry.LastError != entry.Version) {
				return fmt.Errorf("inconsistent snapshot: %+v", entry)
			}
		}
		held := slices.Clone(got)
		runtime.Gosched()
		if !reflect.DeepEqual(got, held) {
			return fmt.Errorf("snapshot changed while held")
		}
	}
	return nil
}

func TestFileOutboxHeldSnapshotsSurviveRecordMutation(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) { checkSnapshotRecordMutation(t, prepared) })
	}
}

func checkSnapshotRecordMutation(t *testing.T, prepared bool) {
	t.Helper()
	outbox, path, active, staged := snapshotFixture(t)
	want := active
	if prepared {
		want = staged
	}
	held := requireSnapshot(t, outbox, prepared, want)
	peer, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	first := want[0]
	if prepared {
		err = peer.RenewPrepared(first.ID, first.PreparedOwner, first.PreparedToken, 2*time.Hour)
	} else {
		err = peer.MarkFailure(first.ID, errors.New("snapshot-peer-error"), time.Time{})
	}
	if err != nil {
		t.Fatal(err)
	}
	got, err := readSnapshot(outbox, prepared)
	if err != nil || len(got) != len(want) {
		t.Fatalf("updated snapshot=%+v error=%v", got, err)
	}
	normalized := slices.Clone(got)
	if prepared {
		if !got[0].PreparedUntil.After(first.PreparedUntil) {
			t.Fatal("prepared lease was not renewed")
		}
		normalized[0].PreparedUntil = first.PreparedUntil
	} else {
		if got[0].Attempts != first.Attempts+1 || got[0].LastError != "snapshot-peer-error" || !got[0].NextAttempt.IsZero() {
			t.Fatalf("failure fields inconsistent: %+v", got[0])
		}
		normalized[0].Attempts, normalized[0].LastError, normalized[0].NextAttempt = first.Attempts, first.LastError, first.NextAttempt
	}
	if !reflect.DeepEqual(normalized, want) || !reflect.DeepEqual(held, want) {
		t.Fatalf("record mutation changed unrelated fields or held snapshot: updated=%+v held=%+v", got, held)
	}
}
