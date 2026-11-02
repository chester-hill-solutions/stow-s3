package runthrough

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOutboxOwnershipCurrentOwner(t *testing.T) {
	for _, action := range ownershipActions {
		for _, expiry := range []string{"future", "expired"} {
			t.Run(action+"/"+expiry, func(t *testing.T) { checkCurrentOwnership(t, action, expiry) })
		}
	}
}

func checkCurrentOwnership(t *testing.T, action, expiry string) {
	t.Helper()
	f := newOwnershipFixture(t, "native", action, ownershipCredentials{})
	var entry OutboxEntry
	var err error
	prepared := strings.HasSuffix(action, "-prepared")
	if prepared {
		if err := f.outbox.DiscardPrepared("intent"); err != nil {
			t.Fatal(err)
		}
		entry, err = f.outbox.PrepareOwned(OutboxEntry{ID: "intent", Operation: OutboxPut, Bucket: "bucket", Key: "key", Version: "original"}, "worker", time.Hour)
	} else {
		var claimed ClaimedEntry
		var acquired bool
		claimed, acquired, err = f.outbox.Claim("intent", "worker", time.Hour)
		entry = claimed.Entry
		if !acquired {
			t.Fatal("could not acquire eligible intent")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	credentials := ownershipCredentials{owner: entry.ClaimOwner, token: entry.ClaimToken}
	if prepared {
		credentials = ownershipCredentials{owner: entry.PreparedOwner, token: entry.PreparedToken}
	}
	if credentials.owner == "" || credentials.token == 0 {
		t.Fatal("allocator returned invalid credentials")
	}
	if expiry == "expired" {
		err = f.outbox.withState(func(s *outboxState) error {
			if prepared {
				entry.PreparedUntil = time.Unix(100, 0).UTC()
				s.prepared[entry.ID] = entry
			} else {
				entry.ClaimUntil = time.Unix(100, 0).UTC()
				s.entries[entry.ID] = entry
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := ownershipAction(f.outbox, action, "intent", credentials)
	if err != nil {
		t.Fatalf("current owner refused: %v", err)
	}
	peer, err := NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	verifyOwnershipResult(t, peer.state, action, got)
}

func verifyOwnershipResult(t *testing.T, state outboxState, action string, got OutboxEntry) {
	t.Helper()
	active, activeOK := state.entries["intent"]
	_, preparedOK := state.prepared["intent"]
	switch action {
	case "success", "discard-prepared":
		if activeOK || preparedOK {
			t.Error("completed intent remains queued")
		}
	case "commit-prepared":
		if !activeOK || preparedOK || active != got || active.Version != "committed" || active.PreparedOwner != "" || active.PreparedToken != 0 {
			t.Error("owned commit did not publish expected active intent")
		}
	case "renew", "renew-prepared":
		verifyOwnershipRenewal(t, state, action)
	case "release", "failure":
		verifyOwnershipRelease(t, active, action)
	}

}

func TestOutboxOwnershipMissingAndCommittedRetries(t *testing.T) {
	for _, action := range ownershipActions {
		t.Run(action, func(t *testing.T) {
			f := newOwnershipFixture(t, "native", action, ownershipCredentials{})
			before := f.outbox.state.clone()
			got, err := ownershipAction(f.outbox, action, "missing", ownershipCredentials{})
			noOp := action == "release" || action == "discard-prepared"
			if noOp && err != nil {
				t.Errorf("missing idempotent operation failed: %v", err)
			}
			if !noOp && (err == nil || errors.Is(err, ErrOutboxClaimLost)) {
				t.Errorf("missing intent error changed: %v", err)
			}
			if got != (OutboxEntry{}) || !reflect.DeepEqual(before, f.outbox.state) {
				t.Error("missing intent operation changed state")
			}
		})
	}
	t.Run("already-committed", func(t *testing.T) {
		f := newOwnershipFixture(t, "native", "success", ownershipCredentials{})
		before := f.outbox.state.clone()
		got, err := f.outbox.CommitPrepared("intent", "", 0, "ignored")
		if err != nil || got != before.entries["intent"] || !reflect.DeepEqual(before, f.outbox.state) {
			t.Fatalf("idempotent committed retry changed: %+v %v", got, err)
		}
	})
}

func verifyOwnershipRenewal(t *testing.T, state outboxState, action string) {
	t.Helper()
	entry, ok := state.entries["intent"]
	owner, token, until := entry.ClaimOwner, entry.ClaimToken, entry.ClaimUntil
	if action == "renew-prepared" {
		entry, ok = state.prepared["intent"]
		owner, token, until = entry.PreparedOwner, entry.PreparedToken, entry.PreparedUntil
	}
	if !ok || owner != "worker" || token == 0 || !until.After(time.Now()) {
		t.Error("lease was not renewed")
	}
}

func verifyOwnershipRelease(t *testing.T, entry OutboxEntry, action string) {
	t.Helper()
	if entry.ID != "intent" || entry.ClaimOwner != "" || entry.ClaimToken != 0 || !entry.ClaimUntil.IsZero() {
		t.Error("claim was not cleared")
	}
	if action == "failure" {
		if entry.Attempts != 1 || entry.LastError != "failed propagation" || entry.NextAttempt.IsZero() || entry.Terminal {
			t.Error("retry fields changed")
		}
	}
}

func TestOutboxOwnershipUnownedAPIs(t *testing.T) {
	for _, format := range []string{"native", "json"} {
		for _, action := range []string{"success", "failure", "commit-prepared", "discard-prepared"} {
			t.Run(format+"/"+action, func(t *testing.T) {
				f := newOwnershipFixture(t, format, action, ownershipCredentials{})
				var got OutboxEntry
				var err error
				switch action {
				case "success":
					err = f.outbox.MarkSuccess("intent")
				case "failure":
					err = f.outbox.MarkFailure("intent", errors.New("failed propagation"), time.Unix(4000000000, 0).UTC())
				case "commit-prepared":
					got, err = f.outbox.Commit("intent", "committed")
				case "discard-prepared":
					err = f.outbox.DiscardPrepared("intent")
				}
				if err != nil {
					t.Fatal(err)
				}
				peer, err := NewFileOutbox(f.path)
				if err != nil {
					t.Fatal(err)
				}
				verifyOwnershipResult(t, peer.state, action, got)
			})
		}
	}
}
