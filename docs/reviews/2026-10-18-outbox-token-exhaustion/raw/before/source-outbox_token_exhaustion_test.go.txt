package runthrough

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

const lastOutboxToken = ^uint64(0)

type tokenFixture struct {
	path   string
	outbox *FileOutbox
	state  outboxState
	data   []byte
}

func newTokenFixture(t *testing.T, format, source string, counter uint64) *tokenFixture {
	t.Helper()
	state := newOutboxState()
	state.seq = 41
	state.nextToken = counter
	for _, id := range []string{"target", "other"} {
		state.entries[id] = OutboxEntry{ID: id, Operation: OutboxPut, Bucket: "bucket", Key: id, Version: "version", CreatedAt: time.Unix(100, 0).UTC()}
	}
	anchor := OutboxEntry{ID: "anchor", Operation: OutboxPut, Bucket: "bucket", Key: "anchor", Version: "anchor-version", CreatedAt: time.Unix(100, 0).UTC(), Terminal: true}
	switch source {
	case "counter":
	case "active-claim":
		state.nextToken = 0
		anchor.ClaimToken = lastOutboxToken
		state.entries[anchor.ID] = anchor
	case "active-prepared":
		state.nextToken = 0
		anchor.PreparedToken = lastOutboxToken
		state.entries[anchor.ID] = anchor
	case "prepared":
		state.nextToken = 0
		anchor.Prepared = true
		anchor.PreparedOwner = "anchor-owner"
		anchor.PreparedToken = lastOutboxToken
		anchor.PreparedUntil = time.Unix(4000000000, 0).UTC()
		state.prepared[anchor.ID] = anchor
	default:
		t.Fatal("unknown token source")
	}
	var data []byte
	var err error
	if format == "native" {
		data, err = encodeOutboxProto(state)
	} else {
		data, err = json.Marshal(persistedOutbox{Version: 3, Entries: state.entries, Prepared: state.prepared, Provenance: state.provenance, Seq: state.seq, NextToken: state.nextToken})
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "outbox")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	return &tokenFixture{path: path, outbox: outbox, state: outbox.state.clone(), data: data}
}
func (f *tokenFixture) unchanged(t *testing.T) {
	t.Helper()
	got, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, f.data) {
		t.Error("refused allocation changed journal bytes")
	}
	if !reflect.DeepEqual(f.outbox.state, f.state) {
		t.Error("refused allocation changed in-memory fields/counters")
	}
	reopened, err := NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.state, f.state) {
		t.Error("refused allocation changed reopened state")
	}
}
func allocateToken(outbox *FileOutbox, action, key string) (OutboxEntry, error) {
	if action == "claim" {
		claim, ok, err := outbox.Claim(key, "same-worker", time.Hour)
		if err == nil && !ok {
			return OutboxEntry{}, fmt.Errorf("eligible claim not acquired")
		}
		return claim.Entry, err
	}
	return outbox.PrepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: key}, "same-worker", time.Hour)
}
func tokenOf(entry OutboxEntry) uint64 {
	if entry.Prepared {
		return entry.PreparedToken
	}
	return entry.ClaimToken
}

func TestOutboxTokenExhaustionRefusesAllocation(t *testing.T) {
	for _, format := range []string{"native", "json"} {
		for _, source := range []string{"counter", "active-claim", "active-prepared", "prepared"} {
			for _, action := range []string{"claim", "prepare"} {
				t.Run(format+"/"+source+"/"+action, func(t *testing.T) {
					f := newTokenFixture(t, format, source, lastOutboxToken)
					key := "target"
					if action == "prepare" {
						key = "new-key"
					}
					got, err := allocateToken(f.outbox, action, key)
					if err == nil || got != (OutboxEntry{}) {
						t.Errorf("exhausted token allocator admitted entry: %+v %v", got, err)
					}
					f.unchanged(t)
				})
			}
		}
	}
}

func TestOutboxTokenExhaustionStaleAcknowledgement(t *testing.T) {
	for _, format := range []string{"native", "json"} {
		t.Run(format, func(t *testing.T) {
			f := newTokenFixture(t, format, "active-claim", lastOutboxToken)
			old, ok, err := f.outbox.Claim("target", "same-worker", time.Hour)
			if err != nil {
				if ok || old != (ClaimedEntry{}) {
					t.Error("failed claim returned ownership")
				}
				f.unchanged(t)
				return
			}
			if !ok {
				t.Fatal("eligible baseline claim not acquired")
			}
			t.Errorf("exhausted counter issued token %d", old.Entry.ClaimToken)
			if err := f.outbox.Release("target", "same-worker", old.Entry.ClaimToken); err != nil {
				t.Fatal(err)
			}
			peer, err := NewFileOutbox(f.path)
			if err != nil {
				t.Fatal(err)
			}
			current, ok, err := peer.Claim("target", "same-worker", time.Hour)
			if err != nil || !ok {
				t.Fatalf("baseline re-claim: %+v %t %v", current, ok, err)
			}
			err = f.outbox.MarkClaimedSuccess("target", "same-worker", old.Entry.ClaimToken)
			if !errors.Is(err, ErrOutboxClaimLost) {
				t.Errorf("stale acknowledgement accepted after token reuse: old=%d current=%d error=%v", old.Entry.ClaimToken, current.Entry.ClaimToken, err)
			}
			pending, err := peer.PendingSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range pending {
				if entry.ID == "target" {
					found = true
				}
			}
			if !found {
				t.Error("stale acknowledgement removed the current propagation intent")
			}
		})
	}
}

func TestOutboxTokenExhaustionLastToken(t *testing.T) {
	for _, format := range []string{"native", "json"} {
		for _, action := range []string{"claim", "prepare"} {
			t.Run(format+"/"+action, func(t *testing.T) {
				f := newTokenFixture(t, format, "counter", lastOutboxToken-1)
				key := "target"
				if action == "prepare" {
					key = "new-key"
				}
				entry, err := allocateToken(f.outbox, action, key)
				if err != nil || tokenOf(entry) != lastOutboxToken {
					t.Fatalf("last valid token: %+v %v", entry, err)
				}
				if action == "claim" {
					err = f.outbox.Release(entry.ID, "same-worker", lastOutboxToken)
				} else {
					err = f.outbox.DiscardPreparedOwned(entry.ID, "same-worker", lastOutboxToken)
				}
				if err != nil {
					t.Fatal(err)
				}
				f.data, err = os.ReadFile(f.path)
				if err != nil {
					t.Fatal(err)
				}
				f.state = f.outbox.state.clone()
				entry, err = allocateToken(f.outbox, action, key)
				if err == nil || entry != (OutboxEntry{}) {
					t.Errorf("last token followed by wrap: %+v %v", entry, err)
				}
				f.unchanged(t)
			})
		}
	}
}

func TestOutboxTokenExhaustionConcurrentLastToken(t *testing.T) {
	for _, format := range []string{"native", "json"} {
		for _, pair := range [][2]string{{"claim", "claim"}, {"claim", "prepare"}, {"prepare", "prepare"}} {
			t.Run(format+"/"+pair[0]+"-"+pair[1], func(t *testing.T) { checkConcurrentLastToken(t, format, pair) })
		}
	}
}
func checkConcurrentLastToken(t *testing.T, format string, pair [2]string) {
	t.Helper()
	f := newTokenFixture(t, format, "counter", lastOutboxToken-1)
	peer, err := NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	var entries [2]OutboxEntry
	var errs [2]error
	start := make(chan struct{})
	var done sync.WaitGroup
	for n, outbox := range []*FileOutbox{f.outbox, peer} {
		done.Add(1)
		go func(n int, o *FileOutbox) {
			defer done.Done()
			<-start
			key := []string{"target", "other"}[n]
			if pair[n] == "prepare" {
				key = "prepared-" + key
			}
			entries[n], errs[n] = allocateToken(o, pair[n], key)
		}(n, outbox)
	}
	close(start)
	done.Wait()
	winners := 0
	for n, entry := range entries {
		if errs[n] == nil {
			winners++
			if tokenOf(entry) != lastOutboxToken {
				t.Errorf("concurrent allocator issued wrapped token: %+v", entry)
			}
		} else if entry != (OutboxEntry{}) {
			t.Error("refused contender returned an entry")
		}
	}
	if winners != 1 {
		t.Errorf("expected one final-token owner; got %d", winners)
	}
	reopened, err := NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.nextToken != lastOutboxToken {
		t.Error("final token counter did not survive peer writes")
	}
}
