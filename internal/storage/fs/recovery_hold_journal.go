package fs

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// readRecoveryHolds returns the recorded holds, or an empty journal for a store
// that never took one. An unreadable journal is an error, not an empty result.
func (s *FilesystemStore) readRecoveryHolds() (recoveryHoldJournal, error) {
	empty := recoveryHoldJournal{Version: 1, Holds: []recoveryHoldEntry{}}
	path := s.recoveryHoldPath()
	stat, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if _, markerErr := os.Lstat(s.recoveryHoldIdentityPath()); !os.IsNotExist(markerErr) {
			return empty, storage.ErrInvalidRecoveryHold
		}
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("%w: %v", storage.ErrInvalidRecoveryHold, err)
	}
	if !stat.Mode().IsRegular() || stat.Size() > maxRecoveryHoldBytes {
		return empty, storage.ErrInvalidRecoveryHold
	}
	file, err := os.Open(path)
	if err != nil {
		return empty, fmt.Errorf("%w: %v", storage.ErrInvalidRecoveryHold, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRecoveryHoldBytes+1))
	if err != nil || len(data) > maxRecoveryHoldBytes {
		return empty, storage.ErrInvalidRecoveryHold
	}
	var journal recoveryHoldJournal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&journal) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return empty, storage.ErrInvalidRecoveryHold
	}
	if err := s.validateRecoveryJournal(journal); err != nil {
		return empty, err
	}
	return journal, nil
}

func (s *FilesystemStore) validateRecoveryJournal(journal recoveryHoldJournal) error {
	if len(journal.Identity) != 32 || journal.Version != 1 || journal.Holds == nil || len(journal.Holds) > maxRecoveryHolds {
		return storage.ErrInvalidRecoveryHold
	}
	if journal.Checksum != recoveryJournalChecksum(journal.Holds) {
		return storage.ErrInvalidRecoveryHold
	}
	if err := s.validateRecoveryIdentity(journal.Identity); err != nil {
		return err
	}
	ids := make(map[string]bool, len(journal.Holds))
	for _, hold := range journal.Holds {
		if hold.ID == "" || ids[hold.ID] || !validRecoveryHoldIdentity(hold.Owner) {
			return storage.ErrInvalidRecoveryHold
		}
		if err := validateRecoveryHoldState(hold); err != nil {
			return err
		}
		ids[hold.ID] = true
	}
	return nil
}

func validateRecoveryHoldState(hold recoveryHoldEntry) error {
	if hold.Created.IsZero() || len(hold.References) == 0 || hold.Meaning == "" {
		return storage.ErrInvalidRecoveryHold
	}
	switch hold.State {
	case "pending":
		if !hold.FinishedAt.IsZero() {
			return storage.ErrInvalidRecoveryHold
		}
	case "released":
		if hold.FinishedAt.IsZero() || hold.FinishedAt.Before(hold.Created) {
			return storage.ErrInvalidRecoveryHold
		}
	default:
		return storage.ErrInvalidRecoveryHold
	}
	return nil
}

func recoveryJournalChecksum(holds []recoveryHoldEntry) string {
	data, _ := json.Marshal(holds)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (s *FilesystemStore) writeRecoveryHolds(journal recoveryHoldJournal) error {
	if journal.Identity == "" {
		identity, err := s.initializeRecoveryHolds()
		if err != nil {
			return err
		}
		journal.Identity = identity
	}
	journal.Checksum = recoveryJournalChecksum(journal.Holds)
	if err := s.validateRecoveryJournal(journal); err != nil {
		return err
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if len(data) > maxRecoveryHoldBytes {
		return storage.ErrRecoveryHoldFull
	}
	if err := atomicfile.Write(s.recoveryHoldPath(), data, 0o600); err != nil {
		return err
	}
	return s.syncRecoveryAncestors()
}

// syncRecoveryAncestors is guarded like every other directory sync here:
// Windows refuses to open a directory for Sync, and the atomic write above is
// what protects the journal where that is not available.
func (s *FilesystemStore) syncRecoveryAncestors() error {
	if !s.SupportsRecoveryHolds() {
		return nil
	}
	return syncSaveAncestors(s.dataDir)
}

func (s *FilesystemStore) validateRecoveryIdentity(expected string) error {
	data, err := os.ReadFile(s.recoveryHoldIdentityPath())
	if err != nil || string(data) != expected {
		return storage.ErrInvalidRecoveryHold
	}
	return nil
}

// initializeRecoveryHolds writes the journal and its identity marker together.
// The marker makes a missing journal detectable rather than indistinguishable
// from a store that never took a hold.
func (s *FilesystemStore) initializeRecoveryHolds() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	identity := hex.EncodeToString(random[:])
	journal := recoveryHoldJournal{Identity: identity, Version: 1, Holds: []recoveryHoldEntry{}}
	journal.Checksum = recoveryJournalChecksum(journal.Holds)
	data, _ := json.Marshal(journal)
	if err := atomicfile.Write(s.recoveryHoldPath(), data, 0o600); err != nil {
		return "", err
	}
	if err := atomicfile.Write(s.recoveryHoldIdentityPath(), []byte(identity), 0o600); err != nil {
		return "", errors.Join(storage.ErrInvalidRecoveryHold, err)
	}
	return identity, s.syncRecoveryAncestors()
}
