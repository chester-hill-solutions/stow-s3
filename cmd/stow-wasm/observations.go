//go:build js && wasm

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	stow "github.com/chester-hill-solutions/stow-s3/internal/embedded"
)

// Observation tokens carry a save condition across the bridge, which the runtime
// refuses to serialize: an etag alone would lose the case where a key is written,
// changed and changed back, which is what a guarded save exists to catch. Bounded
// and expiring like the object MCP tools' tokens, so a long-lived runtime cannot
// grow the table without limit.
const (
	maxObservations = 256
	observationTTL  = 15 * time.Minute
)

var (
	errUnknownObservation = errors.New("observation token is not retained")
	errObservationExpired = errors.New("observation token expired")
)

type observation struct {
	condition stow.SaveCondition
	expires   time.Time
}

// observationTable is one runtime's retained conditions, in insertion order so the
// bound evicts the oldest.
type observationTable struct {
	mu      sync.Mutex
	entries map[string]observation
	order   []string
	now     func() time.Time
}

func newObservationTable() *observationTable {
	return &observationTable{entries: make(map[string]observation), now: time.Now}
}

func newObservationToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (t *observationTable) add(condition stow.SaveCondition) (string, error) {
	token, err := newObservationToken()
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evictExpiredLocked()
	for len(t.entries) >= maxObservations && len(t.order) > 0 {
		oldest := t.order[0]
		t.order = t.order[1:]
		delete(t.entries, oldest)
	}
	t.entries[token] = observation{condition: condition, expires: t.now().Add(observationTTL)}
	t.order = append(t.order, token)
	return token, nil
}

func (t *observationTable) get(token string) (stow.SaveCondition, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[token]
	if !ok {
		return stow.SaveCondition{}, errUnknownObservation
	}
	if t.now().After(entry.expires) {
		delete(t.entries, token)
		return stow.SaveCondition{}, errObservationExpired
	}
	return entry.condition, nil
}

func (t *observationTable) release(token string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, token)
}

func (t *observationTable) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = make(map[string]observation)
	t.order = nil
}

func (t *observationTable) evictExpiredLocked() {
	now := t.now()
	kept := t.order[:0]
	for _, token := range t.order {
		entry, ok := t.entries[token]
		if !ok {
			continue
		}
		if now.After(entry.expires) {
			delete(t.entries, token)
			continue
		}
		kept = append(kept, token)
	}
	t.order = kept
}
