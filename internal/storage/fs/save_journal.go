package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const (
	maxSaveEntries      = 1024
	maxSaveJournalBytes = 8 << 20
	maxSaveEntryBytes   = 64 << 10
)

type saveEntry struct {
	Integrity   string              `json:"integrity"`
	Version     int                 `json:"version"`
	StoreID     string              `json:"store_id"`
	RequestKey  string              `json:"request_key"`
	Bucket      string              `json:"bucket"`
	Key         string              `json:"key"`
	Meaning     string              `json:"meaning"`
	Phase       string              `json:"phase"`
	Meta        *storage.ObjectMeta `json:"meta,omitempty"`
	Fingerprint [sha256.Size]byte   `json:"fingerprint"`
}

type saveState struct {
	id           string
	entries      map[string]saveEntry
	bytes        int
	pending      *saveEntry
	pendingError error
}

func (*FilesystemStore) SupportsGuardedWrites() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}
func (s *FilesystemStore) SupportsDurableSaveRequests() bool { return s.SupportsGuardedWrites() }

func (s *FilesystemStore) saveDir() string     { return filepath.Join(s.dataDir, ".save-requests") }
func (s *FilesystemStore) pendingPath() string { return filepath.Join(s.dataDir, ".save-pending.json") }
func requestName(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:]) + ".json"
}

func validRequestKey(key string) bool {
	return len(key) > 0 && len(key) <= 256 && utf8.ValidString(key) && strings.TrimSpace(key) == key
}

func validSaveOptions(opts storage.PutOptions) bool {
	if !validSaveStrings(opts.ContentType, opts.ChecksumAlgorithm, opts.ChecksumValue, opts.IfMatch, opts.IfNoneMatch) {
		return false
	}
	for key, value := range opts.Metadata {
		if !validSaveStrings(key, value) {
			return false
		}
	}
	return true
}
func validSaveStrings(values ...string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func (s *FilesystemStore) initializeSaves() error {
	s.saves.entries = make(map[string]saveEntry)
	if err := s.loadSaveIdentity(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.saveDir(), 0o700); err != nil {
		return err
	}
	if err := syncSaveAncestors(s.saveDir()); err != nil {
		return err
	}
	if err := s.loadSaveReceipts(); err != nil {
		return err
	}
	pending, _, err := s.readSaveEntry(s.pendingPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		s.saves.pendingError = err
		return nil
	}
	if pending.Phase != "prepared" && pending.Phase != "publishing" {
		s.saves.pendingError = storage.ErrSaveRequestUnknown
		return nil
	}
	s.saves.pending = &pending
	// Unresolved publication remains available to Resolve, but blocks mutation.
	if err := s.settleSaveLocked(); err != nil && !errors.Is(err, storage.ErrSaveRequestUnknown) {
		return err
	}
	return nil
}

func (s *FilesystemStore) loadSaveReceipts() error {
	entries, err := os.ReadDir(s.saveDir())
	if err != nil {
		return err
	}
	if len(entries) > maxSaveEntries {
		return storage.ErrSaveRequestFull
	}
	for _, file := range entries {
		entry, size, err := s.readSaveEntry(filepath.Join(s.saveDir(), file.Name()))
		if err != nil {
			return err
		}
		if file.Name() != requestName(entry.RequestKey) || entry.Phase != "committed" && entry.Phase != "not_committed" {
			return storage.ErrSaveRequestUnknown
		}
		s.saves.entries[entry.RequestKey] = entry
		s.saves.bytes += size
	}
	if s.saves.bytes > maxSaveJournalBytes {
		return storage.ErrSaveRequestFull
	}
	return nil
}

func (s *FilesystemStore) loadSaveIdentity() error {
	idPath := filepath.Join(s.dataDir, ".save-identity.json")
	data, err := os.ReadFile(idPath)
	if os.IsNotExist(err) {
		files, readErr := os.ReadDir(s.saveDir())
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if len(files) != 0 {
			return storage.ErrSaveRequestUnknown
		}
		if _, pendingErr := os.Lstat(s.pendingPath()); !os.IsNotExist(pendingErr) {
			return storage.ErrSaveRequestUnknown
		}
		s.saves.id, err = storage.NewRecordVersion()
		if err == nil {
			err = atomicfile.Write(idPath, []byte(s.saves.id), 0o600)
		}
	} else if err == nil {
		s.saves.id = string(data)
		if !validSaveDigest(s.saves.id, 32) {
			return fmt.Errorf("invalid save store identity")
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func (s *FilesystemStore) readSaveEntry(path string) (saveEntry, int, error) {
	var entry saveEntry
	stat, err := os.Lstat(path)
	if err != nil {
		return entry, 0, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > maxSaveEntryBytes {
		return entry, 0, storage.ErrSaveRequestUnknown
	}
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &entry)
	}
	if err != nil {
		return entry, 0, errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if !validSaveDigest(entry.Integrity, 64) || entry.Integrity != saveEntryIntegrity(entry) {
		return entry, 0, storage.ErrSaveRequestUnknown
	}
	if err := s.validateSaveEntry(entry); err != nil {
		return entry, 0, err
	}
	return entry, len(data), nil
}

func (s *FilesystemStore) validateSaveEntry(entry saveEntry) error {
	if entry.Version != 1 || entry.StoreID != s.saves.id || !validRequestKey(entry.RequestKey) {
		return storage.ErrSaveRequestUnknown
	}
	if storage.ValidateBucketName(entry.Bucket) != nil || storage.ValidateKey(entry.Key) != nil || !validSaveDigest(entry.Meaning, 64) {
		return storage.ErrSaveRequestUnknown
	}
	if entry.Phase == "not_committed" {
		if entry.Meta != nil {
			return storage.ErrSaveRequestUnknown
		}
		return nil
	}
	if entry.Meta == nil {
		return storage.ErrSaveRequestUnknown
	}
	if entry.Meta.Bucket != entry.Bucket || entry.Meta.Key != entry.Key || !validSaveDigest(entry.Meta.VersionID, 32) {
		return storage.ErrSaveRequestUnknown
	}
	return nil
}
func validSaveDigest(value string, size int) bool {
	if len(value) != size {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (s *FilesystemStore) writeSaveEntry(path string, entry saveEntry) (int, error) {
	data, err := encodeSaveEntry(entry)
	if err != nil {
		return 0, err
	}
	if len(data) > maxSaveEntryBytes {
		return 0, storage.ErrSaveRequestFull
	}
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return 0, err
	}
	return len(data), syncSaveAncestors(filepath.Dir(path))
}

func saveEntryIntegrity(entry saveEntry) string {
	entry.Integrity = ""
	data, _ := json.Marshal(entry)
	digest := sha256.Sum256(append([]byte("stow-save-entry-v1\x00"), data...))
	return hex.EncodeToString(digest[:])
}
func encodeSaveEntry(entry saveEntry) ([]byte, error) {
	entry.Integrity = saveEntryIntegrity(entry)
	return json.Marshal(entry)
}

// Each created ancestor entry is fenced, including the store's own parent.
func syncSaveAncestors(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for {
		dir, err := os.Open(path)
		if err != nil {
			return err
		}
		err = dir.Sync()
		closeErr := dir.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func (s *FilesystemStore) faultSave(phase string) error {
	if s.saveFault != nil {
		return s.saveFault(phase)
	}
	return nil
}
