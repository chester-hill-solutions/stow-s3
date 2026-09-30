package fs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const (
	maxRecoveryHolds      = 256
	maxRecoveryReferences = 32
	maxRecoveryHoldBytes  = 2 << 20
)

// A hold pins the bytes one caller observed and still needs. It is on disk
// rather than in memory because the gap it closes is a process dying.
type recoveryHoldReference struct {
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type recoveryHoldEntry struct {
	ID         string                  `json:"id"`
	Owner      string                  `json:"owner"`
	References []recoveryHoldReference `json:"references"`
	Meaning    string                  `json:"meaning"`
	State      string                  `json:"state"`
	Created    time.Time               `json:"created"`
	FinishedAt time.Time               `json:"finished_at,omitempty"`
}

type recoveryHoldJournal struct {
	Identity string              `json:"identity"`
	Version  int                 `json:"version"`
	Holds    []recoveryHoldEntry `json:"holds"`
	Checksum string              `json:"checksum"`
}

func (s *FilesystemStore) SupportsRecoveryHolds() bool { return s.SupportsGuardedWrites() }

func (s *FilesystemStore) recoveryHoldPath() string {
	return filepath.Join(s.dataDir, ".recovery-holds.json")
}

func (s *FilesystemStore) recoveryHoldIdentityPath() string {
	return filepath.Join(s.dataDir, ".recovery-holds.identity")
}

// BeginRecoveryHold records a hold, or confirms one recorded with the same
// meaning. Re-arming a released hold is allowed: the need for the bytes has not
// changed, only the consumer's claim on them lapsed.
func (s *FilesystemStore) BeginRecoveryHold(_ context.Context, options storage.RecoveryHoldOptions) error {
	entry, err := newRecoveryHold(options)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleSaveLocked(); err != nil {
		return err
	}
	journal, err := s.readRecoveryHolds()
	if err != nil {
		return err
	}
	if err := s.observedRecoveryHolds(entry); err != nil {
		return err
	}
	updated := false
	for index, existing := range journal.Holds {
		if existing.ID != entry.ID {
			continue
		}
		if existing.Meaning != entry.Meaning {
			return capacity.ErrConflict
		}
		entry.Created = existing.Created
		journal.Holds[index] = entry
		updated = true
	}
	if !updated {
		if len(journal.Holds) >= maxRecoveryHolds {
			return storage.ErrRecoveryHoldFull
		}
		journal.Holds = append(journal.Holds, entry)
	}
	return s.writeRecoveryHolds(journal)
}

// ReleaseRecoveryHold ends a hold held by the named owner. A replayed release is
// not an error: it is the reply a caller retries, and "not found" cannot be told
// apart from losing the hold.
func (s *FilesystemStore) ReleaseRecoveryHold(_ context.Context, id, owner string) error {
	if !validRecoveryHoldIdentity(id) || !validRecoveryHoldIdentity(owner) {
		return storage.ErrInvalidRecoveryHold
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleSaveLocked(); err != nil {
		return err
	}
	journal, err := s.readRecoveryHolds()
	if err != nil {
		return err
	}
	for index, existing := range journal.Holds {
		if existing.ID != id {
			continue
		}
		if existing.Owner != owner {
			return capacity.ErrConflict
		}
		if existing.State == "released" {
			return nil
		}
		journal.Holds[index].State = "released"
		journal.Holds[index].FinishedAt = time.Now().UTC()
		return s.writeRecoveryHolds(journal)
	}
	return storage.ErrRecoveryHoldNotFound
}

// observedRecoveryHolds refuses a hold whose references no longer describe the
// store: one that does not match what it claims to pin protects nothing at all.
func (s *FilesystemStore) observedRecoveryHolds(entry recoveryHoldEntry) error {
	for _, reference := range entry.References {
		if err := s.observedRecoveryHold(reference); err != nil {
			return err
		}
	}
	return nil
}

func (s *FilesystemStore) observedRecoveryHold(reference recoveryHoldReference) error {
	record, err := readObjectRecord(s.objectPath(reference.Bucket, reference.Key))
	if errors.Is(err, os.ErrNotExist) {
		if reference.Fingerprint == "" {
			return nil
		}
		return storage.ErrInvalidRecoveryHold
	}
	if err != nil {
		return err
	}
	if guardFingerprint(storage.ObjectFingerprint(record.meta(reference.Bucket, reference.Key), record.Data)) != reference.Fingerprint {
		return storage.ErrInvalidRecoveryHold
	}
	return nil
}

// checkRecoveryHoldsLocked refuses a mutation of any pinned object. Reads are
// unaffected: a hold keeps bytes from being replaced, not from being read.
func (s *FilesystemStore) checkRecoveryHoldsLocked(bucket string, keys ...string) error {
	journal, err := s.readRecoveryHolds()
	if err != nil {
		return err
	}
	for _, hold := range journal.Holds {
		if hold.State != "pending" {
			continue
		}
		for _, reference := range hold.References {
			if reference.Bucket == bucket && containsKey(keys, reference.Key) {
				return fmt.Errorf("%w: %s", storage.ErrRecoveryHeld, hold.ID)
			}
		}
	}
	return nil
}

func containsKey(keys []string, key string) bool {
	for _, candidate := range keys {
		if candidate == key {
			return true
		}
	}
	return false
}

func guardFingerprint(fingerprint [sha256.Size]byte) string {
	return hex.EncodeToString(fingerprint[:])
}

func newRecoveryHold(options storage.RecoveryHoldOptions) (recoveryHoldEntry, error) {
	if !validRecoveryHoldIdentity(options.ID) || !validRecoveryHoldIdentity(options.Owner) {
		return recoveryHoldEntry{}, storage.ErrInvalidRecoveryHold
	}
	if len(options.Objects) == 0 || len(options.Objects) > maxRecoveryReferences {
		return recoveryHoldEntry{}, storage.ErrInvalidRecoveryHold
	}
	entry := recoveryHoldEntry{ID: options.ID, Owner: options.Owner, State: "pending", Created: time.Now().UTC()}
	for _, object := range options.Objects {
		reference := recoveryHoldReference{Bucket: object.Bucket, Key: object.Key}
		if !object.Guard.Absent {
			reference.Fingerprint = guardFingerprint(object.Guard.Fingerprint)
		}
		entry.References = append(entry.References, reference)
	}
	if err := normalizeRecoveryReferences(&entry); err != nil {
		return recoveryHoldEntry{}, err
	}
	data, err := json.Marshal(entry.References)
	if err != nil {
		return recoveryHoldEntry{}, err
	}
	digest := sha256.Sum256(append([]byte(options.Owner+"\x00"), data...))
	entry.Meaning = hex.EncodeToString(digest[:])
	return entry, nil
}

// normalizeRecoveryReferences sorts the pinned objects so two callers naming the
// same set in different orders produce one meaning, and refuses a hold claiming
// one object twice and looking like two protections.
func normalizeRecoveryReferences(entry *recoveryHoldEntry) error {
	sort.Slice(entry.References, func(i, j int) bool {
		return entry.References[i].Bucket+"\x00"+entry.References[i].Key < entry.References[j].Bucket+"\x00"+entry.References[j].Key
	})
	for index := 1; index < len(entry.References); index++ {
		if entry.References[index] == entry.References[index-1] {
			return storage.ErrInvalidRecoveryHold
		}
	}
	for _, reference := range entry.References {
		if storage.ValidateBucketName(reference.Bucket) != nil || storage.ValidateKey(reference.Key) != nil {
			return storage.ErrInvalidRecoveryHold
		}
	}
	return nil
}

func validRecoveryHoldIdentity(value string) bool {
	if value == "" || len(value) > 64 || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
