package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const (
	workspaceFileSaveEntryLimit  = 1024
	workspaceFileSaveBytesLimit  = 8 << 20
	workspaceFileSaveRecordLimit = 64 << 10
)

const (
	workspaceFileSavePrepared     = "prepared"
	workspaceFileSavePublishing   = "publishing"
	workspaceFileSavePublished    = "published"
	workspaceFileSaveCommitted    = "committed"
	workspaceFileSaveNotCommitted = "not_committed"
)

var errWorkspaceFileSaveJournalAbsent = errors.New("workspace file-save journal is absent")

type workspaceFileSaveIdentity struct {
	Integrity   string `json:"integrity"`
	Version     int    `json:"version"`
	WorkspaceID string `json:"workspace_id"`
	Bucket      string `json:"bucket"`
	JournalID   string `json:"journal_id"`
	RootDevice  uint64 `json:"root_device"`
	RootInode   uint64 `json:"root_inode"`
}

type workspaceFileSaveProof struct {
	RootDevice   uint64 `json:"root_device"`
	RootInode    uint64 `json:"root_inode"`
	ParentDevice uint64 `json:"parent_device"`
	ParentInode  uint64 `json:"parent_inode"`
	FileDevice   uint64 `json:"file_device"`
	FileInode    uint64 `json:"file_inode"`
	ModifiedNS   int64  `json:"modified_ns"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
	Mode         uint32 `json:"mode"`
}

type workspaceFileSaveRecord struct {
	Integrity        string                  `json:"integrity"`
	Version          int                     `json:"version"`
	WorkspaceID      string                  `json:"workspace_id"`
	Bucket           string                  `json:"bucket"`
	JournalID        string                  `json:"journal_id"`
	RequestKey       string                  `json:"request_key"`
	Path             string                  `json:"path"`
	Meaning          string                  `json:"meaning"`
	Phase            string                  `json:"phase"`
	BasisTokenDigest string                  `json:"basis_token_digest,omitempty"`
	Replace          bool                    `json:"replace,omitempty"`
	PreFileAbsent    bool                    `json:"pre_file_absent"`
	PreEntryIndexed  bool                    `json:"pre_entry_indexed"`
	PreEntry         *ManifestEntry          `json:"pre_entry,omitempty"`
	PlannedEntry     ManifestEntry           `json:"planned_entry"`
	PayloadDigest    string                  `json:"payload_digest"`
	PayloadSize      int64                   `json:"payload_size"`
	Mode             uint32                  `json:"mode"`
	Published        *workspaceFileSaveProof `json:"published,omitempty"`
	Meta             *storage.ObjectMeta     `json:"meta,omitempty"`
}

type workspaceFileSaveJournal struct {
	identity       workspaceFileSaveIdentity
	records        map[string]workspaceFileSaveRecord
	pending        *workspaceFileSaveRecord
	bytes          int
	reserved       int
	pendingBytes   int
	pendingReserve int
}

func (s *Store) initializeFileSaveJournal() (*workspaceFileSaveJournal, error) {
	root, err := s.readRoot()
	if err != nil {
		return nil, uncertain(err)
	}
	defer root.Close()
	return initializeWorkspaceFileSaveJournal(root, s.manifest.WorkspaceID, s.manifest.Bucket)
}

func (s *Store) loadFileSaveJournal() (*workspaceFileSaveJournal, error) {
	root, err := s.readRoot()
	if err != nil {
		return nil, uncertain(err)
	}
	defer root.Close()
	return loadWorkspaceFileSaveJournal(root, s.manifest.WorkspaceID, s.manifest.Bucket)
}

func initializeWorkspaceFileSaveJournal(root *rooted.Root, workspaceID, bucket string) (*workspaceFileSaveJournal, error) {
	if _, err := loadWorkspaceFileSaveJournal(root, workspaceID, bucket); err == nil {
		return nil, storage.ErrSaveRequestConflict
	} else if !errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		return nil, err
	}
	device, inode, ok := fileSaveNativeIdentity(root.Info())
	if !ok || device == 0 || inode == 0 {
		return nil, storage.ErrWorkspaceFileUnsupported
	}
	journalID, err := storage.NewRecordVersion()
	if err != nil {
		return nil, err
	}
	identity := workspaceFileSaveIdentity{Version: 1, WorkspaceID: workspaceID, Bucket: bucket, JournalID: journalID, RootDevice: device, RootInode: inode}
	data, err := encodeWorkspaceFileSaveIdentity(identity)
	if err != nil {
		return nil, err
	}
	if err := root.WriteAtomic(workspaceFileSaveIdentityPath, data, 0o600); err != nil {
		return nil, uncertain(err)
	}
	if err := syncWorkspaceFileSaveNamespace(root, filepath.Dir(workspaceFileSaveIdentityPath)); err != nil {
		return nil, storage.ErrSaveRequestUnknown
	}
	if err := root.Check(); err != nil {
		return nil, storage.ErrSaveRequestUnknown
	}
	return &workspaceFileSaveJournal{identity: identity, records: make(map[string]workspaceFileSaveRecord), bytes: len(data)}, nil
}

func loadWorkspaceFileSaveJournal(root *rooted.Root, workspaceID, bucket string) (*workspaceFileSaveJournal, error) {
	if err := validateWorkspaceFileSaveRoot(root, workspaceID, bucket); err != nil {
		return nil, err
	}
	if err := checkWorkspaceFileSaveDirectory(root); err != nil {
		return nil, err
	}
	identity, identityBytes, err := readWorkspaceFileSaveIdentity(root, workspaceID, bucket)
	if err != nil {
		return nil, err
	}
	journal, err := loadWorkspaceFileSaveContents(root, identity, identityBytes)
	if err != nil {
		return nil, err
	}
	if len(journal.records) > 0 {
		if err := syncWorkspaceFileSaveNamespace(root, workspaceFileSaveReceiptsDir); err != nil {
			return nil, storage.ErrSaveRequestUnknown
		}
	}
	return journal, nil
}

func checkWorkspaceFileSaveDirectory(root *rooted.Root) error {
	metadata, err := root.OpenDirectory(".stow")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errWorkspaceFileSaveJournalAbsent
		}
		return storage.ErrSaveRequestUnknown
	}
	exact, folded, err := findWorkspaceFileSaveMarker(metadata)
	closeErr := metadata.Close()
	if err != nil || closeErr != nil {
		return storage.ErrSaveRequestUnknown
	}
	if folded {
		return storage.ErrSaveRequestUnknown
	}
	if !exact {
		return errWorkspaceFileSaveJournalAbsent
	}
	directory, err := root.OpenDirectory(".stow/file-saves")
	if err != nil {
		return storage.ErrSaveRequestUnknown
	}
	if err := validateWorkspaceFileSaveJournalNames(directory); err != nil {
		directory.Close()
		return err
	}
	if err := directory.Close(); err != nil {
		return storage.ErrSaveRequestUnknown
	}
	return nil
}

func readWorkspaceFileSaveIdentity(root *rooted.Root, workspaceID, bucket string) (workspaceFileSaveIdentity, int, error) {
	identityData, err := readWorkspaceFileSaveFile(root, workspaceFileSaveIdentityPath)
	if errors.Is(err, os.ErrNotExist) || err != nil {
		return workspaceFileSaveIdentity{}, 0, storage.ErrSaveRequestUnknown
	}
	identity, err := decodeWorkspaceFileSaveIdentity(identityData)
	if err != nil || identity.WorkspaceID != workspaceID || identity.Bucket != bucket {
		return workspaceFileSaveIdentity{}, 0, storage.ErrSaveRequestUnknown
	}
	device, inode, ok := fileSaveNativeIdentity(root.Info())
	if !ok || device != identity.RootDevice || inode != identity.RootInode {
		return workspaceFileSaveIdentity{}, 0, storage.ErrSaveRequestUnknown
	}
	return identity, len(identityData), nil
}

func loadWorkspaceFileSaveContents(root *rooted.Root, identity workspaceFileSaveIdentity, identityBytes int) (*workspaceFileSaveJournal, error) {
	journal := &workspaceFileSaveJournal{identity: identity, records: make(map[string]workspaceFileSaveRecord), bytes: identityBytes}
	if err := journal.loadReceipts(root); err != nil {
		return nil, err
	}
	if err := journal.loadPending(root); err != nil {
		return nil, err
	}
	if len(journal.records) > workspaceFileSaveEntryLimit {
		return nil, storage.ErrSaveRequestFull
	}
	if journal.bytes > workspaceFileSaveBytesLimit || journal.reserved > workspaceFileSaveBytesLimit-journal.bytes {
		return nil, storage.ErrSaveRequestFull
	}
	if err := root.Check(); err != nil {
		return nil, storage.ErrSaveRequestUnknown
	}
	return journal, nil
}

func validateWorkspaceFileSaveRoot(root *rooted.Root, workspaceID, bucket string) error {
	if root == nil || !ValidWorkspaceID(workspaceID) || storage.ValidateBucketName(bucket) != nil {
		return storage.ErrInvalidSaveRequest
	}
	if err := root.Check(); err != nil {
		return storage.ErrSaveRequestUnknown
	}
	if _, _, ok := fileSaveNativeIdentity(root.Info()); !ok {
		return storage.ErrWorkspaceFileUnsupported
	}
	return nil
}

func (j *workspaceFileSaveJournal) loadReceipts(root *rooted.Root) error {
	directory, err := root.OpenDirectory(workspaceFileSaveReceiptsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return storage.ErrSaveRequestUnknown
	}
	defer directory.Close()
	for count := 0; ; count++ {
		record, size, done, readErr := j.readNextReceipt(root, directory, count)
		if done {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		j.records[record.RequestKey] = record
		j.bytes += size
	}
}

func (j *workspaceFileSaveJournal) readNextReceipt(root *rooted.Root, directory *os.File, count int) (workspaceFileSaveRecord, int, bool, error) {
	items, err := directory.ReadDir(1)
	if errors.Is(err, io.EOF) {
		return workspaceFileSaveRecord{}, 0, true, nil
	}
	if err != nil || count >= workspaceFileSaveEntryLimit || len(items) != 1 || j.bytes >= workspaceFileSaveBytesLimit {
		return workspaceFileSaveRecord{}, 0, false, storage.ErrSaveRequestUnknown
	}
	return j.decodeReceipt(root, items[0])
}

func (j *workspaceFileSaveJournal) decodeReceipt(root *rooted.Root, item os.DirEntry) (workspaceFileSaveRecord, int, bool, error) {
	if item.IsDir() || !validWorkspaceFileSaveReceiptName(item.Name()) {
		return workspaceFileSaveRecord{}, 0, false, storage.ErrSaveRequestUnknown
	}
	path := filepath.ToSlash(filepath.Join(workspaceFileSaveReceiptsDir, item.Name()))
	data, err := readWorkspaceFileSaveFileLimit(root, path, min(workspaceFileSaveRecordLimit, workspaceFileSaveBytesLimit-j.bytes))
	if err != nil {
		return workspaceFileSaveRecord{}, 0, false, storage.ErrSaveRequestUnknown
	}
	record, err := decodeWorkspaceFileSaveRecord(data)
	if err != nil || j.validateRecordIdentity(record) != nil || record.Phase != workspaceFileSaveCommitted && record.Phase != workspaceFileSaveNotCommitted || item.Name() != workspaceFileSaveReceiptName(record.RequestKey) {
		return workspaceFileSaveRecord{}, 0, false, storage.ErrSaveRequestUnknown
	}
	if _, exists := j.records[record.RequestKey]; exists || len(data) > workspaceFileSaveBytesLimit-j.bytes {
		return workspaceFileSaveRecord{}, 0, false, storage.ErrSaveRequestUnknown
	}
	return record, len(data), false, nil
}

func (j *workspaceFileSaveJournal) loadPending(root *rooted.Root) error {
	data, err := j.readPendingBytes(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return storage.ErrSaveRequestUnknown
	}
	record, err := decodeWorkspaceFileSaveRecord(data)
	if err != nil || j.validateRecordIdentity(record) != nil || record.Phase != workspaceFileSavePrepared && record.Phase != workspaceFileSavePublishing && record.Phase != workspaceFileSavePublished {
		return storage.ErrSaveRequestUnknown
	}
	if terminal, exists := j.records[record.RequestKey]; exists && !workspaceFileSavePendingMatchesReceipt(record, terminal) {
		return storage.ErrSaveRequestUnknown
	}
	j.pending = &record
	j.bytes += len(data)
	j.pendingBytes = len(data)
	if _, receiptExists := j.records[record.RequestKey]; receiptExists {
		return nil
	}
	reserve, err := workspaceFileSaveReservation(record)
	if err != nil || reserve < len(data) {
		return storage.ErrSaveRequestUnknown
	}
	j.reserved = reserve - len(data)
	return nil
}

func (j *workspaceFileSaveJournal) readPendingBytes(root *rooted.Root) ([]byte, error) {
	if j.bytes < workspaceFileSaveBytesLimit {
		return readWorkspaceFileSaveFileLimit(root, workspaceFileSavePendingPath, min(workspaceFileSaveRecordLimit, workspaceFileSaveBytesLimit-j.bytes))
	}
	file, err := root.OpenRegularFile(workspaceFileSavePendingPath)
	if err != nil {
		return nil, err
	}
	file.Close()
	return nil, storage.ErrSaveRequestUnknown
}

func validateWorkspaceFileSaveJournalNames(directory *os.File) error {
	for count := 0; ; count++ {
		entries, err := directory.ReadDir(1)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil || count >= 3 || len(entries) != 1 {
			return storage.ErrSaveRequestUnknown
		}
		entry := entries[0]
		if entry.Name() != "identity.json" && entry.Name() != "pending.json" && entry.Name() != "receipts" {
			return storage.ErrSaveRequestUnknown
		}
		if entry.Name() == "receipts" && !entry.IsDir() {
			return storage.ErrSaveRequestUnknown
		}
		if entry.Name() != "receipts" && entry.IsDir() {
			return storage.ErrSaveRequestUnknown
		}
	}
}

func readWorkspaceFileSaveFile(root *rooted.Root, relative string) ([]byte, error) {
	return readWorkspaceFileSaveFileLimit(root, relative, workspaceFileSaveRecordLimit)
}

func syncWorkspaceFileSaveNamespace(root *rooted.Root, directory string) error {
	for current := directory; ; current = filepath.Dir(current) {
		if current == "" {
			current = "."
		}
		dir, err := root.OpenDirectory(current)
		if err != nil {
			return err
		}
		err = dir.Sync()
		closeErr := dir.Close()
		if err != nil || closeErr != nil {
			return storage.ErrSaveRequestUnknown
		}
		if current == "." {
			return root.Check()
		}
	}
}

func readWorkspaceFileSaveFileLimit(root *rooted.Root, relative string, maximum int) ([]byte, error) {
	file, err := root.OpenRegularFile(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || maximum <= 0 || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 || before.Size() > int64(maximum) {
		return nil, storage.ErrSaveRequestUnknown
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || len(data) > maximum {
		return nil, storage.ErrSaveRequestUnknown
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || int64(len(data)) != after.Size() {
		return nil, storage.ErrSaveRequestUnknown
	}
	return data, nil
}

func findWorkspaceFileSaveMarker(metadata *os.File) (bool, bool, error) {
	exact, folded := false, false
	for count := 0; ; {
		entries, err := metadata.ReadDir(64)
		count += len(entries)
		if count > 4096 {
			return false, false, storage.ErrSaveRequestUnknown
		}
		for _, entry := range entries {
			if entry.Name() == "file-saves" {
				exact = true
			} else if strings.EqualFold(entry.Name(), "file-saves") {
				folded = true
			}
		}
		if errors.Is(err, io.EOF) {
			return exact, folded, nil
		}
		if err != nil {
			return false, false, err
		}
	}
}

func validWorkspaceFileSaveRequestKey(key string) bool {
	return len(key) > 0 && len(key) <= 256 && utf8.ValidString(key) && strings.TrimSpace(key) == key
}

func workspaceFileSaveReceiptName(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:]) + ".json"
}

func validWorkspaceFileSaveReceiptName(name string) bool {
	return len(name) == sha256.Size*2+len(".json") && strings.HasSuffix(name, ".json") && isLowerHex(name[:sha256.Size*2])
}

func isLowerHex(value string) bool {
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

const (
	workspaceFileSaveIdentityPath = ".stow/file-saves/identity.json"
	workspaceFileSavePendingPath  = ".stow/file-saves/pending.json"
	workspaceFileSaveReceiptsDir  = ".stow/file-saves/receipts"
)
