package runthrough

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var ownershipActions = []string{"renew", "release", "success", "failure", "renew-prepared", "commit-prepared", "discard-prepared"}

type ownershipCredentials struct {
	owner string
	token uint64
}

type ownershipFixture struct {
	path   string
	outbox *FileOutbox
}

func newOwnershipFixture(t *testing.T, format, action string, credentials ownershipCredentials) ownershipFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "outbox")
	outbox, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := OutboxEntry{ID: "intent", Operation: OutboxPut, Bucket: "bucket", Key: "key", Version: "original", CreatedAt: time.Unix(100, 0).UTC()}
	if strings.HasSuffix(action, "-prepared") {
		entry.PreparedOwner, entry.PreparedToken = credentials.owner, credentials.token
		_, err = outbox.Prepare(entry)
	} else {
		entry.ClaimOwner, entry.ClaimToken = credentials.owner, credentials.token
		_, err = outbox.Enqueue(entry)
	}
	if err != nil {
		t.Fatal(err)
	}
	if format == "json" {
		s := outbox.state
		data, err := json.Marshal(persistedOutbox{Version: outboxLegacyJSONVersion, Entries: s.entries, Prepared: s.prepared, Provenance: s.provenance, Seq: s.seq, NextToken: s.nextToken})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	outbox, err = NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	return ownershipFixture{path: path, outbox: outbox}
}

func ownershipAction(o *FileOutbox, action, id string, credentials ownershipCredentials) (OutboxEntry, error) {
	switch action {
	case "renew":
		return OutboxEntry{}, o.Renew(id, credentials.owner, credentials.token, time.Hour)
	case "release":
		return OutboxEntry{}, o.Release(id, credentials.owner, credentials.token)
	case "success":
		return OutboxEntry{}, o.MarkClaimedSuccess(id, credentials.owner, credentials.token)
	case "failure":
		return OutboxEntry{}, o.MarkClaimedFailure(id, credentials.owner, credentials.token, errors.New("failed propagation"), time.Unix(4000000000, 0).UTC())
	case "renew-prepared":
		return OutboxEntry{}, o.RenewPrepared(id, credentials.owner, credentials.token, time.Hour)
	case "commit-prepared":
		return o.CommitPrepared(id, credentials.owner, credentials.token, "committed")
	case "discard-prepared":
		return OutboxEntry{}, o.DiscardPreparedOwned(id, credentials.owner, credentials.token)
	default:
		return OutboxEntry{}, errors.New("unknown ownership action")
	}
}

func TestOutboxOwnershipRequiresIssuedCredentials(t *testing.T) {
	invalid := []struct {
		name        string
		credentials ownershipCredentials
	}{
		{"unowned", ownershipCredentials{}},
		{"empty-owner", ownershipCredentials{token: 41}},
		{"zero-token", ownershipCredentials{owner: "worker"}},
	}
	for _, format := range []string{"native", "json"} {
		for _, action := range ownershipActions {
			for _, invalid := range invalid {
				t.Run(format+"/"+action+"/"+invalid.name, func(t *testing.T) {
					f := newOwnershipFixture(t, format, action, invalid.credentials)
					assertOwnershipRefused(t, f, action, invalid.credentials)
				})
			}
		}
	}
}

func assertOwnershipRefused(t *testing.T, f ownershipFixture, action string, credentials ownershipCredentials) {
	t.Helper()
	before := f.outbox.state.clone()
	data, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ownershipAction(f.outbox, action, "intent", credentials)
	if !errors.Is(err, ErrOutboxClaimLost) {
		t.Errorf("non-owner operation accepted: entry=%+v error=%v", got, err)
	}
	if got != (OutboxEntry{}) {
		t.Error("refused operation returned a committed intent")
	}
	actual, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, actual) {
		t.Error("refused operation changed journal bytes")
	}
	if !reflect.DeepEqual(before, f.outbox.state) {
		t.Error("refused operation changed in-memory state")
	}
	peer, err := NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, peer.state) {
		t.Error("refused operation changed reopened state")
	}
}

func TestOutboxOwnershipRejectsMismatchedCredentials(t *testing.T) {
	for _, format := range []string{"native", "json"} {
		for _, action := range ownershipActions {
			for _, invalid := range []ownershipCredentials{{owner: "other", token: 41}, {owner: "worker", token: 42}, {}} {
				t.Run(format+"/"+action+"/"+invalid.owner+"-"+time.Duration(invalid.token).String(), func(t *testing.T) {
					f := newOwnershipFixture(t, format, action, ownershipCredentials{owner: "worker", token: 41})
					assertOwnershipRefused(t, f, action, invalid)
				})
			}
		}
	}
}
