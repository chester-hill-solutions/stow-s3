package runthrough

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type admissionFixture struct {
	outbox   CoordinatedOutbox
	path     string
	original OutboxEntry
}

func admitEntry(outbox CoordinatedOutbox, method string, entry OutboxEntry) (OutboxEntry, error) {
	switch method {
	case "enqueue":
		return outbox.Enqueue(entry)
	case "prepare":
		return outbox.Prepare(entry)
	default:
		return outbox.(OwnedPreparedOutbox).PrepareOwned(entry, "challenger", time.Hour)
	}
}

func newAdmissionFixture(t *testing.T, backend, state string) admissionFixture {
	t.Helper()
	f := admissionFixture{outbox: NewMemoryOutbox()}
	if backend == "file" {
		f.path = filepath.Join(t.TempDir(), "outbox")
		outbox, err := NewFileOutbox(f.path)
		if err != nil {
			t.Fatal(err)
		}
		f.outbox = outbox
	}
	entry := OutboxEntry{ID: "outbox-41", Operation: OutboxPut, Bucket: "bucket", Key: "original", Version: "original-version", CreatedAt: time.Unix(100, 0).UTC()}
	var err error
	switch state {
	case "prepared":
		f.original, err = f.outbox.Prepare(entry)
	case "owned":
		f.original, err = f.outbox.(OwnedPreparedOutbox).PrepareOwned(entry, "original-owner", time.Hour)
	default:
		f.original, err = f.outbox.Enqueue(entry)
	}
	if err != nil {
		t.Fatal(err)
	}
	if state == "claimed" {
		claimed, ok, err := f.outbox.(ClaimableOutbox).Claim(entry.ID, "original-owner", time.Hour)
		if err != nil || !ok {
			t.Fatalf("claim fixture: %+v %t %v", claimed, ok, err)
		}
		f.original = claimed.Entry
	}
	return f
}

func admissionStates(backend string) []string {
	if backend == "file" {
		return []string{"active", "claimed", "prepared", "owned"}
	}
	return []string{"active", "prepared"}
}

func admissionMethods(backend string) []string {
	if backend == "file" {
		return []string{"enqueue", "prepare", "owned"}
	}
	return []string{"enqueue", "prepare"}
}

func TestOutboxAdmissionRejectsOccupiedIDs(t *testing.T) {
	for _, backend := range []string{"memory", "file"} {
		for _, state := range admissionStates(backend) {
			for _, method := range admissionMethods(backend) {
				for _, identical := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/identical=%t", backend, state, method, identical), func(t *testing.T) {
						f := newAdmissionFixture(t, backend, state)
						checkOccupiedAdmission(t, f, method, identical)
					})
				}
			}
		}
	}
}

func checkOccupiedAdmission(t *testing.T, f admissionFixture, method string, identical bool) {
	t.Helper()
	wantPending, wantPrepared := f.outbox.Pending(), f.outbox.Prepared()
	var before []byte
	if f.path != "" {
		var err error
		before, err = os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
	}
	candidate := f.original
	if !identical {
		candidate = OutboxEntry{ID: f.original.ID, Operation: OutboxDelete, Bucket: "bucket", Key: "replacement", Version: "replacement-version"}
	}
	accepted, err := admitEntry(f.outbox, method, candidate)
	if err == nil {
		t.Errorf("occupied ID accepted: method=%s returned=%+v", method, accepted)
	}
	if err != nil && !reflect.DeepEqual(accepted, OutboxEntry{}) {
		t.Errorf("failure returned entry: %+v", accepted)
	}
	pending, prepared := f.outbox.Pending(), f.outbox.Prepared()
	if !reflect.DeepEqual(pending, wantPending) || !reflect.DeepEqual(prepared, wantPrepared) {
		t.Errorf("protected intent changed: pending=%+v prepared=%+v", pending, prepared)
	}
	if f.path != "" {
		checkAdmissionFile(t, f.path, before)
	}
	if err == nil {
		return
	}
	next, err := f.outbox.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "next", Version: "next-version"})
	if err != nil || next.ID != "outbox-42" {
		t.Fatalf("failed admission changed sequence: %+v %v", next, err)
	}
	if file, ok := f.outbox.(*FileOutbox); ok {
		checkAdmissionFence(t, file, f.original)
	}
}

func checkAdmissionFile(t *testing.T, path string, before []byte) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Errorf("rejected admission changed journal: rewritten=%t error=%v", !bytes.Equal(after, before), err)
	}
	if _, err := NewFileOutbox(path); err != nil {
		t.Errorf("journal cannot reopen: %v", err)
	}
}

func checkAdmissionFence(t *testing.T, outbox *FileOutbox, original OutboxEntry) {
	t.Helper()
	switch {
	case original.ClaimOwner != "":
		if err := outbox.Renew(original.ID, original.ClaimOwner, original.ClaimToken, time.Hour); err != nil {
			t.Fatal(err)
		}
	case original.PreparedOwner != "":
		if err := outbox.RenewPrepared(original.ID, original.PreparedOwner, original.PreparedToken, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	next, err := outbox.PrepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "next-owned"}, "new-owner", time.Hour)
	previousToken := max(original.ClaimToken, original.PreparedToken)
	if err != nil || next.ID != "outbox-43" || next.PreparedToken != previousToken+1 {
		t.Fatalf("failed admission changed fencing: %+v %v", next, err)
	}
}

func TestOutboxAdmissionConcurrentSameID(t *testing.T) {
	for _, backend := range []string{"memory", "file", "peer"} {
		methods := admissionMethods("file")
		if backend == "memory" {
			methods = admissionMethods("memory")
		}
		for _, first := range methods {
			for _, second := range methods {
				t.Run(backend+"/"+first+"/"+second, func(t *testing.T) { checkAdmissionRace(t, backend, first, second) })
			}
		}
	}
}

func checkAdmissionRace(t *testing.T, backend, first, second string) {
	t.Helper()
	kind := "file"
	if backend == "memory" {
		kind = "memory"
	}
	f := newAdmissionFixture(t, kind, "active")
	peers := []CoordinatedOutbox{f.outbox, f.outbox}
	if backend == "peer" {
		peer, err := NewFileOutbox(f.path)
		if err != nil {
			t.Fatal(err)
		}
		peers[1] = peer
	}
	results := make([]OutboxEntry, 2)
	errors := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, method := range []string{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errors[i] = admitEntry(peers[i], method, OutboxEntry{ID: "contended", Operation: OutboxPut, Bucket: "bucket", Key: fmt.Sprintf("contender-%d", i), Version: "candidate-version"})
		}()
	}
	close(start)
	wg.Wait()
	winner, accepted := -1, 0
	for i, err := range errors {
		if err == nil {
			winner, accepted = i, accepted+1
		} else if results[i] != (OutboxEntry{}) {
			t.Errorf("loser returned entry: %+v", results[i])
		}
	}
	if accepted != 1 {
		t.Fatalf("same ID accepted %d times: errors=%v", accepted, errors)
	}
	inspectAdmissionWinner(t, f, results[winner])
}

func inspectAdmissionWinner(t *testing.T, f admissionFixture, winner OutboxEntry) {
	t.Helper()
	inspect := f.outbox
	if f.path != "" {
		var err error
		inspect, err = NewFileOutbox(f.path)
		if err != nil {
			t.Fatal(err)
		}
	}
	all := append(inspect.Pending(), inspect.Prepared()...)
	found := 0
	for _, entry := range all {
		if entry.ID == "contended" {
			found++
			if !reflect.DeepEqual(entry, winner) {
				t.Errorf("winner changed: %+v want %+v", entry, winner)
			}
		}
	}
	if found != 1 || len(all) != 2 {
		t.Fatalf("wrong admitted intent count: %+v", all)
	}
}

func TestOutboxAdmissionAllowsAvailableIDs(t *testing.T) {
	for _, backend := range []string{"memory", "file"} {
		for _, method := range admissionMethods(backend) {
			t.Run(backend+"/"+method, func(t *testing.T) { checkAvailableAdmission(t, backend, method) })
		}
	}
}

func checkAvailableAdmission(t *testing.T, backend, method string) {
	t.Helper()
	f := newAdmissionFixture(t, backend, "active")
	entry, err := admitEntry(f.outbox, method, OutboxEntry{ID: "custom/雪\x00id", Operation: OutboxPut, Bucket: "bucket", Key: "fresh-custom", Version: "fresh-version"})
	if err != nil || entry.ID != "custom/雪\x00id" {
		t.Fatalf("fresh custom ID refused: %+v %v", entry, err)
	}
	entry = commitAvailableAdmission(t, f.outbox, method, entry)
	if err := f.outbox.MarkSuccess(entry.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := f.outbox.Enqueue(OutboxEntry{ID: entry.ID, Operation: OutboxPut, Bucket: "bucket", Key: "after-completion", Version: "next-version"})
	if err != nil || replacement.Key != "after-completion" {
		t.Fatalf("available completed ID refused: %+v %v", replacement, err)
	}
	next, err := f.outbox.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "automatic", Version: "automatic"})
	if err != nil || next.ID != "outbox-42" {
		t.Fatalf("automatic sequence changed: %+v %v", next, err)
	}
	if err := f.outbox.Close(); err != nil {
		t.Fatal(err)
	}
	if f.path != "" {
		if _, err := NewFileOutbox(f.path); err != nil {
			t.Fatal(err)
		}
	}
}

func commitAvailableAdmission(t *testing.T, outbox CoordinatedOutbox, method string, entry OutboxEntry) OutboxEntry {
	t.Helper()
	var err error
	if method != "enqueue" {
		if method == "owned" {
			entry, err = outbox.(OwnedPreparedOutbox).CommitPrepared(entry.ID, entry.PreparedOwner, entry.PreparedToken, "committed-version")
		} else {
			entry, err = outbox.Commit(entry.ID, "committed-version")
		}
		if err != nil {
			t.Fatal(err)
		}
		again, err := outbox.Commit(entry.ID, "ignored-retry-version")
		if err != nil || !reflect.DeepEqual(again, entry) {
			t.Fatalf("idempotent commit changed: %+v %v", again, err)
		}
	}
	return entry
}
