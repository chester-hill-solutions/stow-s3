package storage

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
)

var ErrSaveConflict = errors.New("guarded save conflicts with current object")

// GuardedWriteStore qualifies atomic comparison with a managed mutation.
type GuardedWriteStore interface {
	SupportsGuardedWrites() bool
}

// WriteGuard binds an observation of bytes and object properties.
type WriteGuard struct {
	Absent      bool
	Fingerprint [sha256.Size]byte
}

// ObjectFingerprint includes the generation, so A→B→A remains a change.
func ObjectFingerprint(meta ObjectMeta, data []byte) [sha256.Size]byte {
	properties, _ := json.Marshal(struct {
		Generation, ContentType, ChecksumAlgorithm, ChecksumValue string
		Metadata                                                  map[string]string
		Content                                                   [sha256.Size]byte
	}{meta.VersionID, meta.ContentType, meta.ChecksumAlgorithm, meta.ChecksumValue, meta.Metadata, sha256.Sum256(data)})
	return sha256.Sum256(properties)
}

func (*MemoryStore) SupportsGuardedWrites() bool { return true }

func checkMemoryGuard(guard *WriteGuard, existing *memObject) error {
	if guard == nil {
		return nil
	}
	if guard.Absent {
		if existing == nil {
			return nil
		}
	} else if existing != nil && guard.Fingerprint == ObjectFingerprint(existing.meta, existing.data) {
		return nil
	}
	return ErrSaveConflict
}
