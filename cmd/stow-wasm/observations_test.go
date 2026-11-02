//go:build js && wasm

package main

import (
	"errors"
	stow "github.com/chester-hill-solutions/stow-s3/internal/embedded"
	"testing"
	"time"
)

func TestObservationTokensExpireAtTheirDeadline(t *testing.T) {
	table := newObservationTable()
	now := time.Unix(100, 0)
	table.now = func() time.Time { return now }
	token, err := table.add(stow.ReplacementCondition())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(observationTTL - time.Nanosecond)
	if _, err := table.get(token); err != nil {
		t.Fatalf("unexpired token refused: %v", err)
	}
	now = now.Add(time.Nanosecond)
	if _, err := table.get(token); !errors.Is(err, errObservationExpired) {
		t.Fatalf("token at deadline: %v, want expiration", err)
	}
}

func TestObservationTableBoundsReleaseAndClear(t *testing.T) {
	table := newObservationTable()
	first, err := table.add(stow.ReplacementCondition())
	if err != nil {
		t.Fatal(err)
	}
	last := first
	for range maxObservations {
		last, err = table.add(stow.ReplacementCondition())
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(table.entries) != maxObservations || len(table.order) != maxObservations {
		t.Fatalf("retained entries/order = %d/%d", len(table.entries), len(table.order))
	}
	if _, err := table.get(first); !errors.Is(err, errUnknownObservation) {
		t.Fatalf("oldest token not evicted: %v", err)
	}
	if _, err := table.get(last); err != nil {
		t.Fatalf("newest token refused: %v", err)
	}
	table.release(last)
	if _, err := table.get(last); !errors.Is(err, errUnknownObservation) {
		t.Fatalf("released token retained: %v", err)
	}
	table.clear()
	if len(table.entries) != 0 || len(table.order) != 0 {
		t.Fatal("close retained observations")
	}
}

func TestExpiredObservationsAreCompactedOnInsertion(t *testing.T) {
	table := newObservationTable()
	now := time.Unix(100, 0)
	table.now = func() time.Time { return now }
	old, err := table.add(stow.ReplacementCondition())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(observationTTL)
	if _, err := table.add(stow.ReplacementCondition()); err != nil {
		t.Fatal(err)
	}
	if len(table.entries) != 1 || len(table.order) != 1 {
		t.Fatal("expired observations retained at insertion")
	}
	if _, err := table.get(old); !errors.Is(err, errUnknownObservation) {
		t.Fatalf("expired observation survived insertion: %v", err)
	}
}

func TestClosedRuntimeCannotRecreateObservationTable(t *testing.T) {
	instance, err := stow.Open(stow.Options{})
	if err != nil {
		t.Fatal(err)
	}
	state.Lock()
	handle := state.next
	state.next++
	state.instances[handle] = instance
	state.observations[handle] = newObservationTable()
	state.Unlock()
	// A read may already have obtained its condition when close removes its handle.
	// Its subsequent table lookup must not recreate retained state for that handle.
	if _, err := handleClose(nil, instance, request{Handle: handle}); err != nil {
		t.Fatal(err)
	}
	table := observationsFor(handle)
	state.Lock()
	_, retained := state.observations[handle]
	state.Unlock()
	if table != nil || retained {
		t.Fatal("closed runtime recreated its observation table")
	}
}
