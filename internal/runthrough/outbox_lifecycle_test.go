package runthrough

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOutboxLifecycleTransitionsKeepHistorySeparate(t *testing.T) {
	for _, backend := range []string{"memory", "file"} {
		t.Run(backend, func(t *testing.T) { exerciseOutboxLifecycle(t, lifecycleOutbox(t, backend)) })
	}
}

func lifecycleOutbox(t *testing.T, backend string) CoordinatedOutbox {
	t.Helper()
	if backend == "memory" {
		return NewMemoryOutbox()
	}
	file, err := NewFileOutbox(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	return file
}

func exerciseOutboxLifecycle(t *testing.T, outbox CoordinatedOutbox) {
	t.Helper()
	entry, err := outbox.Prepare(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "key"})
	if err != nil {
		t.Fatal(err)
	}
	assertOutboxPhases(t, entry, outboxPrepared, outboxReady)
	if len(outbox.Pending()) != 0 {
		t.Fatal("prepared publication was admitted to propagation")
	}
	entry, err = outbox.Commit(entry.ID, "local-version")
	if err != nil {
		t.Fatal(err)
	}
	assertOutboxPhases(t, entry, outboxPublished, outboxReady)
	if entry.Version != "local-version" || entry.Attempted {
		t.Fatal("publication changed propagation history")
	}
	exerciseOutboxFailures(t, outbox, entry)
}

func exerciseOutboxFailures(t *testing.T, outbox CoordinatedOutbox, entry OutboxEntry) {
	t.Helper()
	when := time.Now().Add(time.Hour)
	if err := outbox.MarkFailure(entry.ID, NewTransientUpstreamError(errors.New("unavailable")), when); err != nil {
		t.Fatal(err)
	}
	entry = outbox.Pending()[0]
	assertOutboxPhases(t, entry, outboxPublished, outboxRetryScheduled)
	if entry.Attempts != 1 || !entry.NextAttempt.Equal(when) || entry.LastError != "unavailable" {
		t.Fatalf("retry did not retain its schedule/history: %+v", entry)
	}
	if err := outbox.MarkFailure(entry.ID, ErrUpstreamConflict, when); err != nil {
		t.Fatal(err)
	}
	entry = outbox.Pending()[0]
	assertOutboxPhases(t, entry, outboxPublished, outboxTerminal)
	if !entry.NextAttempt.IsZero() || entry.Attempts != 2 {
		t.Fatalf("terminal failure retained a retry schedule: %+v", entry)
	}
	if err := outbox.MarkSuccess(entry.ID); err != nil {
		t.Fatal(err)
	}
	if len(outbox.Pending()) != 0 {
		t.Fatal("acknowledgement retained a completed intent")
	}
}

func assertOutboxPhases(t *testing.T, entry OutboxEntry, publication outboxPublicationPhase, propagation outboxPropagationPhase) {
	t.Helper()
	if entry.publicationPhase() != publication || entry.propagationPhase() != propagation {
		t.Fatalf("unexpected lifecycle: publication=%v propagation=%v entry=%+v", entry.publicationPhase(), entry.propagationPhase(), entry)
	}
}

func TestOutboxClaimTransitionsFenceInterruptedWorkers(t *testing.T) {
	state := newOutboxState()
	entry, err := state.enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "key"})
	if err != nil {
		t.Fatal(err)
	}
	first, acquired, err := state.claim(entry.ID, "first", time.Hour)
	if err != nil || !acquired || first.Reconcile || !first.Entry.Attempted {
		t.Fatalf("first claim = %+v, %t, %v", first, acquired, err)
	}
	assertOutboxPhases(t, first.Entry, outboxPublished, outboxReady)
	if err := state.markSuccess(entry.ID); !errors.Is(err, ErrOutboxClaimHeld) {
		t.Fatalf("unowned acknowledgement bypassed claim: %v", err)
	}
	second := recoverReleasedOutboxClaim(t, &state, first)

	if err := state.markClaimedFailure(entry.ID, "second", second.Entry.ClaimToken, ErrUpstreamConflict, time.Time{}); err != nil {
		t.Fatal(err)
	}
	terminal := state.entries[entry.ID]
	assertOutboxPhases(t, terminal, outboxPublished, outboxTerminal)
	if terminal.propagationLease().present() || !terminal.Attempted {
		t.Fatal("terminal transition lost history or retained ownership")
	}
	if _, acquired, err := state.claim(entry.ID, "third", time.Hour); err != nil || acquired {
		t.Fatalf("terminal intent became claimable: %t, %v", acquired, err)
	}
	if err := state.discard(entry.ID); err != nil {
		t.Fatal(err)
	}
}

func recoverReleasedOutboxClaim(t *testing.T, state *outboxState, first ClaimedEntry) ClaimedEntry {
	t.Helper()
	id := first.Entry.ID
	if err := state.releaseClaim(id, "first", first.Entry.ClaimToken); err != nil {
		t.Fatal(err)
	}
	released := state.entries[id]
	if released.propagationLease().present() || !released.Attempted {
		t.Fatal("release forgot propagation history or retained ownership")
	}
	second, acquired, err := state.claim(id, "second", time.Hour)
	if err != nil || !acquired || !second.Reconcile || second.Entry.ClaimToken <= first.Entry.ClaimToken {
		t.Fatalf("recovered claim = %+v, %t, %v", second, acquired, err)
	}
	if err := state.markClaimedSuccess(id, "first", first.Entry.ClaimToken); !errors.Is(err, ErrOutboxClaimLost) {
		t.Fatalf("stale worker acknowledged a new claim: %v", err)
	}
	return second
}

func TestOutboxPreparedReopenPreservesOwnedCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox")
	first, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := first.PrepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "key", Attempted: true}, "worker", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Commit(entry.ID, "wrong"); !errors.Is(err, ErrOutboxClaimHeld) {
		t.Fatalf("legacy commit bypassed preparation ownership: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	retained := reopened.Prepared()[0]
	if !reflect.DeepEqual(retained, entry) {
		t.Fatal("restart changed the prepared record")
	}
	committed, err := reopened.CommitPrepared(entry.ID, "worker", entry.PreparedToken, "local-version")
	if err != nil {
		t.Fatal(err)
	}
	assertOutboxPhases(t, committed, outboxPublished, outboxReady)
	if committed.preparationLease().present() || committed.PreparedToken != 0 || !committed.PreparedUntil.IsZero() || !committed.Attempted {
		t.Fatal("commit retained its preparation lease or erased attempt history")
	}
	claim, acquired, err := reopened.Claim(entry.ID, "recovery", time.Hour)
	if err != nil || !acquired || !claim.Reconcile {
		t.Fatalf("restart forgot reconciliation obligation: %+v, %t, %v", claim, acquired, err)
	}
}

func TestOutboxPhaseCompatibility(t *testing.T) {
	when := time.Unix(100, 0).UTC()
	for _, item := range []struct {
		name        string
		entry       OutboxEntry
		publication outboxPublicationPhase
		propagation outboxPropagationPhase
	}{
		{"ready", OutboxEntry{}, outboxPublished, outboxReady},
		{"prepared", OutboxEntry{Prepared: true}, outboxPrepared, outboxReady},
		{"retry", OutboxEntry{NextAttempt: when}, outboxPublished, outboxRetryScheduled},
		{"historical attempt", OutboxEntry{Attempted: true}, outboxPublished, outboxReady},
		{"terminal with legacy schedule", OutboxEntry{Terminal: true, NextAttempt: when}, outboxPublished, outboxTerminal},
		{"prepared terminal", OutboxEntry{Prepared: true, Terminal: true}, outboxPrepared, outboxTerminal},
	} {
		t.Run(item.name, func(t *testing.T) { assertOutboxPhases(t, item.entry, item.publication, item.propagation) })
	}
	entry := OutboxEntry{Terminal: true, Attempted: true, Attempts: 2}
	entry.recordPropagationFailure(NewTransientUpstreamError(errors.New("retry")), when)
	if entry.propagationPhase() != outboxTerminal || entry.NextAttempt != when || entry.Attempts != 3 || !entry.Attempted {
		t.Fatal("transient bookkeeping reopened a terminal intent or changed legacy fields")
	}
}
