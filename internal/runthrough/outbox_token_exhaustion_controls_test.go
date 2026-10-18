package runthrough

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestOutboxTokenExhaustionStateAtomicity(t *testing.T) {
	for _, action := range []string{"claim", "prepare"} {
		t.Run(action, func(t *testing.T) {
			s := newOutboxState()
			s.seq = 41
			s.nextToken = lastOutboxToken
			s.entries["target"] = OutboxEntry{ID: "target", Operation: OutboxPut, Bucket: "bucket", Key: "target", Version: "v"}
			before := s.clone()
			var entry OutboxEntry
			var err error
			if action == "claim" {
				var claim ClaimedEntry
				var ok bool
				claim, ok, err = s.claim("target", "owner", time.Hour)
				entry = claim.Entry
				if ok {
					t.Error("exhausted state granted ownership")
				}
			} else {
				entry, err = s.prepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "new"}, "owner", time.Hour)
			}
			if err == nil || entry != (OutboxEntry{}) {
				t.Errorf("exhausted state admitted entry: %+v %v", entry, err)
			}
			if !reflect.DeepEqual(s, before) {
				t.Error("exhausted allocation consumed an ID/token or changed fields")
			}
		})
	}
}

func TestOutboxTokenExhaustionExistingOwners(t *testing.T) {
	for _, format := range []string{"native", "json"} {
		for _, action := range []string{"renew", "release", "acknowledge", "renew-prepared", "commit-prepared", "discard-prepared"} {
			t.Run(format+"/"+action, func(t *testing.T) { checkExistingTokenOwner(t, format, action) })
		}
	}
}
func checkExistingTokenOwner(t *testing.T, format, action string) {
	t.Helper()
	f := newTokenFixture(t, format, "counter", lastOutboxToken)
	prepared := action == "renew-prepared" || action == "commit-prepared" || action == "discard-prepared"
	err := f.outbox.withState(func(s *outboxState) error {
		entry := s.entries["target"]
		entry.Attempted = true
		if prepared {
			delete(s.entries, entry.ID)
			entry.Prepared = true
			entry.PreparedOwner = "existing-owner"
			entry.PreparedToken = lastOutboxToken
			entry.PreparedUntil = time.Now().UTC().Add(time.Hour)
			s.prepared[entry.ID] = entry
		} else {
			entry.ClaimOwner = "existing-owner"
			entry.ClaimToken = lastOutboxToken
			entry.ClaimUntil = time.Now().UTC().Add(time.Hour)
			s.entries[entry.ID] = entry
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	switch action {
	case "renew":
		err = f.outbox.Renew("target", "existing-owner", lastOutboxToken, time.Hour)
	case "release":
		err = f.outbox.Release("target", "existing-owner", lastOutboxToken)
	case "acknowledge":
		err = f.outbox.MarkClaimedSuccess("target", "existing-owner", lastOutboxToken)
	case "renew-prepared":
		err = f.outbox.RenewPrepared("target", "existing-owner", lastOutboxToken, time.Hour)
	case "commit-prepared":
		_, err = f.outbox.CommitPrepared("target", "existing-owner", lastOutboxToken, "committed")
	case "discard-prepared":
		err = f.outbox.DiscardPreparedOwned("target", "existing-owner", lastOutboxToken)
	}
	if err != nil {
		t.Fatalf("exhaustion prevented existing owner completion: %v", err)
	}
	reopened, err := NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.nextToken != lastOutboxToken {
		t.Error("owner completion reset exhausted token counter")
	}
}

func TestOutboxTokenExhaustionPreconditions(t *testing.T) {
	for _, format := range []string{"native", "json"} {
		for _, kind := range []string{"missing", "held", "terminal", "bad-claim-lease", "bad-prepared-owner", "bad-prepared-lease", "prepared-key", "duplicate-id"} {
			t.Run(format+"/"+kind, func(t *testing.T) { checkExhaustionPrecondition(t, format, kind) })
		}
	}
}
func checkExhaustionPrecondition(t *testing.T, format, kind string) {
	t.Helper()
	f := newTokenFixture(t, format, "counter", lastOutboxToken)
	if kind == "held" || kind == "terminal" || kind == "prepared-key" {
		if err := f.outbox.withState(func(s *outboxState) error {
			entry := s.entries["target"]
			switch kind {
			case "held":
				entry.ClaimOwner = "existing-owner"
				entry.ClaimToken = 12
				entry.ClaimUntil = time.Now().UTC().Add(time.Hour)
				s.entries[entry.ID] = entry
			case "terminal":
				entry.Terminal = true
				s.entries[entry.ID] = entry
			case "prepared-key":
				delete(s.entries, entry.ID)
				entry.Prepared = true
				entry.PreparedOwner = "existing-owner"
				entry.PreparedToken = 12
				s.prepared[entry.ID] = entry
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var err error
		f.data, err = os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		f.state = f.outbox.state.clone()
	}
	checkTokenPreconditionResult(t, f, kind)
	if kind == "missing" || kind == "held" || kind == "terminal" {
		var err error
		f.data, err = os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
	}
	f.unchanged(t)
}
func checkTokenPreconditionResult(t *testing.T, f *tokenFixture, kind string) {
	t.Helper()
	if kind == "missing" || kind == "held" || kind == "terminal" {
		id := "target"
		if kind == "missing" {
			id = "absent"
		}
		entry, ok, err := f.outbox.Claim(id, "owner", time.Hour)
		if err != nil || ok || entry != (ClaimedEntry{}) {
			t.Fatalf("ineligible claim changed behavior: %+v %t %v", entry, ok, err)
		}
		return
	}
	if kind == "bad-claim-lease" {
		_, _, err := f.outbox.Claim("target", "owner", 0)
		if !errors.Is(err, ErrOutboxClaimLease) {
			t.Fatalf("claim lease precedence: %v", err)
		}
		return
	}
	checkPreparedTokenPrecondition(t, f, kind)
}
func checkPreparedTokenPrecondition(t *testing.T, f *tokenFixture, kind string) {
	t.Helper()
	entry := OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "new"}
	owner, lease := "owner", time.Hour
	var want error
	switch kind {
	case "bad-prepared-owner":
		owner = ""
		want = ErrOutboxClaimLease
	case "bad-prepared-lease":
		lease = 0
		want = ErrOutboxClaimLease
	case "prepared-key":
		entry.Key = "target"
		want = ErrOutboxPreparedUnresolved
	case "duplicate-id":
		entry.ID = "target"
	}
	got, err := f.outbox.PrepareOwned(entry, owner, lease)
	if err == nil || got != (OutboxEntry{}) {
		t.Fatalf("invalid prepare admitted: %+v %v", got, err)
	}
	if want != nil && !errors.Is(err, want) {
		t.Fatalf("prepare error precedence changed: %v", err)
	}
}

func TestOutboxTokenExhaustionIDFailureDoesNotConsumeToken(t *testing.T) {
	for _, kind := range []string{"duplicate", "id-limit"} {
		t.Run(kind, func(t *testing.T) {
			s := newOutboxState()
			s.seq = 41
			s.nextToken = lastOutboxToken - 1
			s.entries["target"] = OutboxEntry{ID: "target", Bucket: "bucket", Key: "original"}
			entry := OutboxEntry{Bucket: "bucket", Key: "new"}
			if kind == "duplicate" {
				entry.ID = "target"
			} else {
				s.seq = lastOutboxToken
			}
			before := s.clone()
			got, err := s.prepareOwned(entry, "owner", time.Hour)
			if err == nil || got != (OutboxEntry{}) {
				t.Fatalf("invalid ID admitted: %+v %v", got, err)
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("ID refusal consumed fencing token")
			}
		})
	}
}

func TestOutboxTokenExhaustionStateLastToken(t *testing.T) {
	for _, action := range []string{"claim", "prepare"} {
		t.Run(action, func(t *testing.T) {
			s := newOutboxState()
			s.seq = 41
			s.nextToken = lastOutboxToken - 1
			s.entries["target"] = OutboxEntry{ID: "target", Operation: OutboxPut, Bucket: "bucket", Key: "target", Version: "v"}
			if action == "claim" {
				claim, ok, err := s.claim("target", "owner", time.Hour)
				if err != nil || !ok || claim.Entry.ClaimToken != lastOutboxToken {
					t.Fatalf("last state claim: %+v %t %v", claim, ok, err)
				}
			} else {
				entry, err := s.prepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "new"}, "owner", time.Hour)
				if err != nil || entry.PreparedToken != lastOutboxToken {
					t.Fatalf("last state prepare: %+v %v", entry, err)
				}
			}
			if s.nextToken != lastOutboxToken {
				t.Fatal("last issued token did not advance shared state")
			}
			before := s.clone()
			entry, err := s.prepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "another"}, "owner", time.Hour)
			if err == nil || entry != (OutboxEntry{}) {
				t.Errorf("state reused exhausted token: %+v %v", entry, err)
			}
			if !reflect.DeepEqual(before, s) {
				t.Error("refusal after last token changed state")
			}
		})
	}
}
