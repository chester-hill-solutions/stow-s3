package runthrough

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestFileOutboxOwnWriteMatchesReopenedState(t *testing.T) {
	for _, method := range []string{"enqueue", "prepare"} {
		for _, kind := range []string{"ordinary", "text", "monotonic-time", "offset-time", "historical-attempt", "counter-fields"} {
			t.Run(method+"/"+kind, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "outbox")
				outbox, err := NewFileOutbox(path)
				if err != nil {
					t.Fatal(err)
				}
				entry := ownWriteCanonicalFixture(kind)
				if method == "enqueue" {
					_, err = outbox.Enqueue(entry)
				} else {
					_, err = outbox.Prepare(entry)
				}
				if err != nil {
					t.Fatal(err)
				}
				peer, err := NewFileOutbox(path)
				if err != nil {
					t.Fatal(err)
				}
				got, err := outbox.PendingSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				want, err := peer.PendingSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("own-write active snapshot differs from reopened state: got=%+v want=%+v", got, want)
				}
				got, err = outbox.PreparedSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				want, err = peer.PreparedSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("own-write prepared snapshot differs from reopened state: got=%+v want=%+v", got, want)
				}
				if outbox.state.seq != peer.state.seq || outbox.state.nextToken != peer.state.nextToken {
					t.Fatal("own-write counters differ from reopened state")
				}
			})
		}
	}
}

func TestFileOutboxOwnWritePreservesCounterFences(t *testing.T) {
	for _, method := range []string{"enqueue", "prepare"} {
		t.Run(method, func(t *testing.T) {
			outbox, err := NewFileOutbox(filepath.Join(t.TempDir(), "outbox"))
			if err != nil {
				t.Fatal(err)
			}
			entry := OutboxEntry{ID: "outbox-41", Operation: OutboxPut, Bucket: "bucket", Key: "key", Version: "version", ClaimToken: 900, PreparedToken: 901}
			if method == "enqueue" {
				entry, err = outbox.Enqueue(entry)
				if err != nil {
					t.Fatal(err)
				}
				claimed, ok, err := outbox.Claim(entry.ID, "next-owner", time.Hour)
				if err != nil || !ok || claimed.Entry.ClaimToken != 902 {
					t.Fatalf("claim reused a fencing token after own write: %+v %t %v", claimed, ok, err)
				}
			} else {
				if _, err = outbox.Prepare(entry); err != nil {
					t.Fatal(err)
				}
				next, err := outbox.PrepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "another"}, "next-owner", time.Hour)
				if err != nil || next.ID != "outbox-42" || next.PreparedToken != 902 {
					t.Fatalf("owned prepare reused a counter after own write: %+v %v", next, err)
				}
			}
		})
	}
}

func TestFileOutboxOwnWriteCanonicalizesProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox")
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.SaveUpstreamState("bucket", "key", UpstreamState{ETag: "tag-\xff"}); err != nil {
		t.Fatal(err)
	}
	got, known, err := outbox.UpstreamState("bucket", "key")
	if err != nil || !known || got.ETag != "tag-\ufffd" {
		t.Fatalf("own-write provenance not canonical: %+v %t %v", got, known, err)
	}
}

func TestFileOutboxOwnWriteRejectsChangedPeerBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox")
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Enqueue(OutboxEntry{ID: "intent", Operation: OutboxPut, Bucket: "bucket", Key: "key", Version: "version"}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("STOWOUT4"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.PendingSnapshot(); err == nil {
		t.Fatal("own-write cache hid changed corrupt journal")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.PendingSnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.PendingSnapshot(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("own-write cache hid missing journal: %v", err)
	}
}

func TestFileOutboxOwnWriteReloadsAfterUncertainFence(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix unprivileged permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox")
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Enqueue(OutboxEntry{ID: "original", Operation: OutboxPut, Bucket: "bucket", Key: "original", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	before, err := outbox.PendingSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0333); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	_, err = outbox.Enqueue(OutboxEntry{ID: "uncertain", Operation: OutboxPut, Bucket: "bucket", Key: "uncertain", Version: "v2"})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("directory fence did not fail: %v", err)
	}
	if !reflect.DeepEqual(outbox.state.pending(), before) {
		t.Fatal("uncertain write changed acknowledged in-memory state")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	peer, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := outbox.PendingSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	want, err := peer.PendingSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 2 || !reflect.DeepEqual(got, want) {
		t.Fatal("own-write cache hid the visible uncertain replacement")
	}
}

func ownWriteCanonicalFixture(kind string) OutboxEntry {
	entry := OutboxEntry{ID: "outbox-41", Operation: OutboxPut, Bucket: "bucket", Key: "key", Version: "version", CreatedAt: time.Unix(100, 0).UTC()}
	switch kind {
	case "text":
		entry.Key = "key-\xff"
		entry.Version = "version-\xfe"
		entry.LastError = "error-\xfd"
		entry.ClaimOwner = "owner-\xff"
		entry.PreparedOwner = "prepared-\xfe"
	case "monotonic-time":
		entry.CreatedAt = time.Now()
		entry.NextAttempt = time.Now().Add(time.Hour)
	case "offset-time":
		entry.CreatedAt = time.Date(2026, 10, 18, 1, 2, 3, 4, time.FixedZone("seconds-offset", 3661))
		entry.NextAttempt = entry.CreatedAt.Add(time.Hour)
	case "historical-attempt":
		entry.Attempts = 3
	case "counter-fields":
		entry.ClaimToken = 900
		entry.PreparedToken = 901
	}
	return entry
}
