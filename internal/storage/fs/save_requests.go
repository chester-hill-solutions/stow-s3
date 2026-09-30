package fs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *FilesystemStore) saveMeaning(bucket, key string, data []byte, opts storage.PutOptions) string {
	encoded, _ := json.Marshal(struct {
		StoreID, Bucket, Key, Operation, ContentType, ChecksumAlgorithm, ChecksumValue, IfMatch, IfNoneMatch string
		Metadata                                                                                             map[string]string
		Guard                                                                                                *storage.WriteGuard
		Payload                                                                                              [sha256.Size]byte
	}{s.saves.id, bucket, key, "save-v1", opts.ContentType, storage.NormalizeChecksumAlgorithm(opts.ChecksumAlgorithm), opts.ChecksumValue, opts.IfMatch, opts.IfNoneMatch, storage.CloneMetadata(opts.Metadata), opts.Guard, sha256.Sum256(data)})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (s *FilesystemStore) ReplaySaveRequest(ctx context.Context, bucket, key string, data []byte, opts storage.PutOptions) (storage.SaveReceipt, bool, error) {
	if ctx != nil && ctx.Err() != nil {
		return storage.SaveReceipt{}, false, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replaySaveLocked(bucket, key, data, opts)
}

func (s *FilesystemStore) replaySaveLocked(bucket, key string, data []byte, opts storage.PutOptions) (storage.SaveReceipt, bool, error) {
	if !s.SupportsDurableSaveRequests() {
		return storage.SaveReceipt{}, false, storage.ErrSaveRequestsUnsupported
	}
	if !validRequestKey(opts.RequestKey) || !validSaveOptions(opts) || !validSaveStrings(bucket, key) {
		return storage.SaveReceipt{}, false, storage.ErrInvalidSaveRequest
	}
	meaning := s.saveMeaning(bucket, key, data, opts)
	if pending := s.saves.pending; pending != nil && pending.RequestKey == opts.RequestKey && pending.Meaning != meaning {
		return storage.SaveReceipt{}, true, storage.ErrSaveRequestConflict
	}
	if entry, found := s.saves.entries[opts.RequestKey]; found {
		if entry.Meaning != meaning {
			return storage.SaveReceipt{Outcome: "unknown"}, true, storage.ErrSaveRequestConflict
		}
		return entryReceipt(entry), true, entryError(entry)
	}
	if err := s.settleSaveLocked(); err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, true, err
	}
	entry, found := s.saves.entries[opts.RequestKey]
	if !found {
		return storage.SaveReceipt{}, false, nil
	}
	if entry.Meaning != meaning {
		return storage.SaveReceipt{Outcome: "unknown"}, true, storage.ErrSaveRequestConflict
	}
	return entryReceipt(entry), true, entryError(entry)
}

func (s *FilesystemStore) ResolveSaveRequest(ctx context.Context, bucket, key, requestKey string) (storage.SaveReceipt, error) {
	if ctx != nil && ctx.Err() != nil {
		return storage.SaveReceipt{}, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.SupportsDurableSaveRequests() {
		return storage.SaveReceipt{}, storage.ErrSaveRequestsUnsupported
	}
	if !validRequestKey(requestKey) || !validSaveStrings(bucket, key) {
		return storage.SaveReceipt{}, storage.ErrInvalidSaveRequest
	}
	if entry, found := s.saves.entries[requestKey]; found {
		if entry.Bucket != bucket || entry.Key != key {
			return storage.SaveReceipt{}, storage.ErrSaveRequestConflict
		}
		return entryReceipt(entry), entryError(entry)
	}
	if pending := s.saves.pending; pending != nil && pending.RequestKey == requestKey && (pending.Bucket != bucket || pending.Key != key) {
		return storage.SaveReceipt{}, storage.ErrSaveRequestConflict
	}
	if err := s.settleSaveLocked(); err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	if entry, found := s.saves.entries[requestKey]; found {
		return entryReceipt(entry), entryError(entry)
	}
	return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestNotFound
}

func entryReceipt(entry saveEntry) storage.SaveReceipt {
	var meta *storage.ObjectMeta
	if entry.Meta != nil {
		copy := *entry.Meta
		copy.Metadata = storage.CloneMetadata(copy.Metadata)
		meta = &copy
	}
	return storage.SaveReceipt{Meta: meta, Outcome: entry.Phase, Replayed: true}
}
func entryError(entry saveEntry) error {
	if entry.Phase == "not_committed" {
		return storage.ErrSaveRequestNotCommitted
	}
	return nil
}

func (s *FilesystemStore) commitSaveLocked(bucket, key string, record objectRecord, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	meta := record.meta(bucket, key)
	entry := saveEntry{Version: 1, StoreID: s.saves.id, RequestKey: opts.RequestKey, Bucket: bucket, Key: key, Meaning: s.saveMeaning(bucket, key, record.Data, opts), Phase: "prepared", Meta: &meta, Fingerprint: storage.ObjectFingerprint(meta, record.Data)}
	reserve, err := saveEntryReservation(entry)
	if err != nil || len(s.saves.entries) >= maxSaveEntries || s.saves.bytes+reserve > maxSaveJournalBytes {
		return nil, storage.ErrSaveRequestFull
	}
	s.saves.pending = &entry
	if _, err := s.writeSaveEntry(s.pendingPath(), entry); err != nil {
		return nil, errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if err := s.faultSave("prepared"); err != nil {
		return nil, errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	entry.Phase = "publishing"
	if _, err := s.writeSaveEntry(s.pendingPath(), entry); err != nil {
		return nil, errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if err := s.faultSave("publishing"); err != nil {
		return nil, errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	record.SaveRequest = entry.RequestKey
	record.SaveMeaning = entry.Meaning
	if err := s.writeObjectRaw(bucket, key, record); err != nil {
		return nil, errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if err := s.faultSave("published"); err != nil {
		return nil, errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}
	if err := s.faultSave("terminal"); err != nil {
		return &meta, storage.CommittedError(err)
	}
	return &meta, nil
}

func saveEntryReservation(entry saveEntry) (int, error) {
	maximum := 0
	for _, phase := range []string{"prepared", "publishing", "committed", "not_committed"} {
		entry.Phase = phase
		encoded, err := encodeSaveEntry(entry)
		if err != nil {
			return 0, err
		}
		if len(encoded) > maxSaveEntryBytes {
			return 0, storage.ErrSaveRequestFull
		}
		if len(encoded) > maximum {
			maximum = len(encoded)
		}
	}
	return 2 * maximum, nil
}

func (s *FilesystemStore) settleSaveLocked() error {
	if s.saves.pendingError != nil {
		return errors.Join(storage.ErrSaveRequestUnknown, s.saves.pendingError)
	}
	if s.saves.pending == nil {
		return nil
	}
	entry := *s.saves.pending
	if retained, found := s.saves.entries[entry.RequestKey]; found {
		if !sameSaveIntent(entry, retained) {
			return storage.ErrSaveRequestUnknown
		}
		return s.finalizeSaveLocked(retained)
	}
	if entry.Phase == "publishing" {
		if err := s.verifyPublishedSaveLocked(entry); err != nil {
			return err
		}
		entry.Phase = "committed"
	} else if entry.Phase == "prepared" {
		entry.Phase = "not_committed"
		entry.Meta = nil
	} else {
		return storage.ErrSaveRequestUnknown
	}
	return s.finalizeSaveLocked(entry)
}

func sameSaveIntent(pending, retained saveEntry) bool {
	if pending.StoreID != retained.StoreID || pending.Bucket != retained.Bucket || pending.Key != retained.Key || pending.Meaning != retained.Meaning || pending.Fingerprint != retained.Fingerprint {
		return false
	}
	if retained.Phase == "not_committed" {
		return pending.Phase == "prepared"
	}
	if pending.Phase != "publishing" || retained.Phase != "committed" {
		return false
	}
	left, _ := json.Marshal(pending.Meta)
	right, _ := json.Marshal(retained.Meta)
	return string(left) == string(right)
}

func (s *FilesystemStore) finalizeSaveLocked(entry saveEntry) error {
	path := filepath.Join(s.saveDir(), requestName(entry.RequestKey))
	size, err := s.writeSaveEntry(path, entry)
	if err != nil {
		return errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if _, exists := s.saves.entries[entry.RequestKey]; !exists {
		s.saves.bytes += size
	}
	s.saves.entries[entry.RequestKey] = entry
	if err := os.Remove(s.pendingPath()); err != nil && !os.IsNotExist(err) {
		return errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if err := syncSaveAncestors(s.dataDir); err != nil {
		return errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	s.saves.pending = nil
	return nil
}

func (s *FilesystemStore) verifyPublishedSaveLocked(entry saveEntry) error {
	record, err := s.readObject(entry.Bucket, entry.Key)
	if err != nil || record.SaveRequest != entry.RequestKey || record.SaveMeaning != entry.Meaning || storage.ObjectFingerprint(record.meta(entry.Bucket, entry.Key), record.Data) != entry.Fingerprint {
		return errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	planned, _ := json.Marshal(entry.Meta)
	meta := record.meta(entry.Bucket, entry.Key)
	actual, _ := json.Marshal(&meta)
	if string(planned) != string(actual) {
		return storage.ErrSaveRequestUnknown
	}
	if err := s.faultSave("sync"); err != nil {
		return errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if err := s.writeObjectRaw(entry.Bucket, entry.Key, record); err != nil {
		return errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if err := syncSaveAncestors(filepath.Dir(s.objectPath(entry.Bucket, entry.Key))); err != nil {
		return errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	return nil
}
