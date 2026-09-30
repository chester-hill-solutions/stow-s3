package workspace

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
)

type recoveryHoldJournal struct {
	Identity string         `json:"identity"`
	Version  int            `json:"version"`
	Holds    []RecoveryHold `json:"holds"`
	Checksum string         `json:"checksum"`
}

func (r *Registry) recoveryHoldPath() string {
	return filepath.Join(r.dir, ".stow", "recovery-holds.json")
}

func (r *Registry) readRecoveryHolds() (recoveryHoldJournal, error) {
	empty := recoveryHoldJournal{Version: 1, Holds: []RecoveryHold{}}
	path := r.recoveryHoldPath()
	stat, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if _, markerErr := os.Lstat(r.recoveryIdentityPath()); !os.IsNotExist(markerErr) {
			return empty, ErrRecoveryHoldCorrupt
		}
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("%w: %v", ErrRecoveryHoldCorrupt, err)
	}
	if !stat.Mode().IsRegular() || stat.Size() > MaxRecoveryHoldBytes {
		return empty, ErrRecoveryHoldCorrupt
	}
	file, err := os.Open(path)
	if err != nil {
		return empty, fmt.Errorf("%w: %v", ErrRecoveryHoldCorrupt, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxRecoveryHoldBytes+1))
	if err != nil || len(data) > MaxRecoveryHoldBytes {
		return empty, ErrRecoveryHoldCorrupt
	}
	var journal recoveryHoldJournal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&journal) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return empty, ErrRecoveryHoldCorrupt
	}
	if err := r.validateRecoveryIdentity(journal.Identity); err != nil {
		return empty, err
	}
	if err := validateRecoveryJournal(journal); err != nil {
		return empty, fmt.Errorf("%w: %v", ErrRecoveryHoldCorrupt, err)
	}
	return journal, nil
}

func validateRecoveryJournal(journal recoveryHoldJournal) error {
	if len(journal.Identity) != 32 || journal.Version != 1 || journal.Holds == nil || len(journal.Holds) > MaxRecoveryHolds || journal.Checksum != recoveryChecksum(journal.Holds) {
		return ErrRecoveryHoldCorrupt
	}
	var previous string
	for _, hold := range journal.Holds {
		request, meaning, err := normalizeRecoveryHold(hold.RecoveryHoldRequest)
		if err != nil || meaning != hold.Meaning || !reflect.DeepEqual(request, hold.RecoveryHoldRequest) || hold.ID <= previous || hold.Created.IsZero() {
			return ErrRecoveryHoldCorrupt
		}
		if err := validateRecoveryState(hold); err != nil {
			return err
		}
		previous = hold.ID
	}
	if recoveryJournalReserve(journal) > MaxRecoveryHoldBytes {
		return ErrRecoveryHoldFull
	}
	return nil
}

func validateRecoveryState(hold RecoveryHold) error {
	switch hold.State {
	case "pending":
		if !hold.FinishedAt.IsZero() {
			return ErrRecoveryHoldCorrupt
		}
	case "released", "discarded":
		if hold.FinishedAt.IsZero() || hold.FinishedAt.Before(hold.Created) {
			return ErrRecoveryHoldCorrupt
		}
	default:
		return ErrRecoveryHoldCorrupt
	}
	return nil
}

func recoveryChecksum(holds []RecoveryHold) string {
	data, _ := json.Marshal(holds)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func recoveryJournalReserve(journal recoveryHoldJournal) int64 {
	reserved := int64(256)
	for _, hold := range journal.Holds {
		reserved += recoveryHoldReserve(hold)
	}
	return reserved
}

func (r *Registry) writeRecoveryHolds(journal recoveryHoldJournal) error {
	if journal.Identity == "" {
		identity, err := r.initializeRecoveryHolds()
		if err != nil {
			return err
		}
		journal.Identity = identity
	}
	journal.Checksum = recoveryChecksum(journal.Holds)
	if err := validateRecoveryJournal(journal); err != nil {
		return err
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if len(data) > MaxRecoveryHoldBytes {
		return ErrRecoveryHoldFull
	}
	if err := os.MkdirAll(filepath.Dir(r.recoveryHoldPath()), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(r.recoveryHoldPath(), data, 0o600)
}

func (r *Registry) recoveryIdentityPath() string {
	return filepath.Join(r.dir, ".stow", "recovery-holds.identity")
}

func (r *Registry) validateRecoveryIdentity(expected string) error {
	stat, err := os.Lstat(r.recoveryIdentityPath())
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != 32 {
		return ErrRecoveryHoldCorrupt
	}
	data, err := os.ReadFile(r.recoveryIdentityPath())
	if err != nil || string(data) != expected {
		return ErrRecoveryHoldCorrupt
	}
	return nil
}

func (r *Registry) initializeRecoveryHolds() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	identity := hex.EncodeToString(random[:])
	journal := recoveryHoldJournal{Identity: identity, Version: 1, Holds: []RecoveryHold{}}
	journal.Checksum = recoveryChecksum(journal.Holds)
	data, _ := json.Marshal(journal)
	if err := atomicfile.Write(r.recoveryHoldPath(), data, 0o600); err != nil {
		return "", err
	}
	if err := atomicfile.Write(r.recoveryIdentityPath(), []byte(identity), 0o600); err != nil {
		return "", err
	}
	return identity, syncRegistryDirectory(r.dir)
}
