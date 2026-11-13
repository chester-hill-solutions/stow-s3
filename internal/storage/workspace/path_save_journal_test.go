package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceFileSaveJournalRoundTripAndNoPayloadStorage(t *testing.T) {
	store := newFileSaveJournalStore(t)
	manifestBefore := readWorkspaceSaveJournalFile(t, manifestPath(store.root))
	indexBefore := readWorkspaceSaveJournalFile(t, indexPath(store.root))
	root, journal := createFileSaveJournal(t, store)
	defer root.Close()
	record := newFileSaveJournalRecord(store, journal, "unique payload body")
	if err := journal.reserve(record); err != nil {
		t.Fatal(err)
	}
	if err := journal.writePending(root, record); err != nil {
		t.Fatal(err)
	}
	prepared, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket)
	if err != nil || prepared.pending == nil || prepared.pending.Phase != workspaceFileSavePrepared || prepared.reserved == 0 {
		t.Fatalf("loaded prepared journal=%+v error=%v", prepared, err)
	}
	completeWorkspaceFileSaveJournal(t, store, root, journal, record)
	if !bytes.Equal(manifestBefore, readWorkspaceSaveJournalFile(t, manifestPath(store.root))) || !bytes.Equal(indexBefore, readWorkspaceSaveJournalFile(t, indexPath(store.root))) {
		t.Fatal("journal codec changed workspace manifest or object index")
	}
	if _, err := os.Lstat(filepath.Join(store.root, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("journal codec changed payload path: %v", err)
	}
}

func completeWorkspaceFileSaveJournal(t *testing.T, store *Store, root *rooted.Root, journal *workspaceFileSaveJournal, record workspaceFileSaveRecord) {
	t.Helper()
	record.Phase = workspaceFileSavePublishing
	if err := journal.writePending(root, record); err != nil {
		t.Fatal(err)
	}
	premature := record
	premature.Phase = workspaceFileSaveNotCommitted
	if err := journal.writePending(root, premature); !errors.Is(err, storage.ErrInvalidSaveRequest) {
		t.Fatalf("publishing to not-committed transition=%v", err)
	}
	if err := journal.writeReceipt(root, premature); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("publishing not-committed receipt=%v", err)
	}
	proof := testWorkspaceFileSaveProof(t, root, record.PayloadDigest, record.PayloadSize, record.Mode)
	record.Phase, record.Published = workspaceFileSavePublished, &proof
	record.PlannedEntry.Modified = time.Unix(0, proof.ModifiedNS).UTC().Truncate(time.Second)
	if err := journal.writePending(root, record); err != nil {
		t.Fatal(err)
	}
	meta := store.metaFromEntry(record.Bucket, record.Path, record.PlannedEntry)
	meta.LastModified = time.Unix(0, proof.ModifiedNS).UTC()
	record.Phase, record.Meta = workspaceFileSaveCommitted, meta
	wrongTime := record
	wrongMeta := *meta
	wrongMeta.LastModified = record.PlannedEntry.Modified
	wrongTime.Meta = &wrongMeta
	if _, err := encodeWorkspaceFileSaveRecord(wrongTime); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("committed meta with truncated nanoseconds=%v", err)
	}
	if err := journal.writeReceipt(root, record); err != nil {
		t.Fatal(err)
	}
	terminalPending, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket)
	if err != nil || terminalPending.pending == nil || terminalPending.reserved != 0 {
		t.Fatalf("receipt-before-clear state=%+v error=%v", terminalPending, err)
	}
	terminal, found := terminalPending.lookup(record.RequestKey)
	if !found || terminal.Phase != workspaceFileSaveCommitted {
		t.Fatalf("terminal lookup with pending slot=%+v found=%v", terminal, found)
	}
	if err := journal.clearPending(root); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket)
	if err != nil || loaded.pending != nil {
		t.Fatalf("loaded terminal journal=%+v error=%v", loaded, err)
	}
	assertWorkspaceFileSaveReceipt(t, store, loaded, record)
}

func assertWorkspaceFileSaveReceipt(t *testing.T, store *Store, loaded *workspaceFileSaveJournal, record workspaceFileSaveRecord) {
	t.Helper()
	got, ok := loaded.lookup(record.RequestKey)
	if !ok || got.Phase != workspaceFileSaveCommitted || got.Meta == nil || got.Meta.VersionID != record.PlannedEntry.VersionID {
		t.Fatalf("receipt=%+v found=%v", got, ok)
	}
	encoded, err := os.ReadFile(filepath.Join(store.root, workspaceFileSaveReceiptsDir, workspaceFileSaveReceiptName(record.RequestKey)))
	if err != nil || bytes.Contains(encoded, []byte("unique payload body")) || bytes.Contains(encoded, []byte("opaque observation token")) {
		t.Fatalf("receipt leaked payload or token: bytes=%d error=%v", len(encoded), err)
	}
	info, err := os.Stat(filepath.Join(store.root, workspaceFileSaveIdentityPath))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("identity mode=%v error=%v", info, err)
	}
}

func TestWorkspaceFileSaveJournalRefusesMissingCorruptAndForeignIdentity(t *testing.T) {
	store := newFileSaveJournalStore(t)
	root, err := store.readRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket); !errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		t.Fatalf("missing journal load=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(store.root, workspaceFileSaveIdentityPath)); !os.IsNotExist(err) {
		t.Fatalf("missing lookup created journal marker: %v", err)
	}
	if err := os.Mkdir(filepath.Join(store.root, ".stow", "file-saves"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("marker without identity load=%v", err)
	}
	if err := os.Remove(filepath.Join(store.root, ".stow", "file-saves")); err != nil {
		t.Fatal(err)
	}
	journal, err := initializeWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	assertWorkspaceFileSaveJournalRejectsInvalidIdentity(t, store, root, journal.identity)
}

func TestWorkspaceFileSaveJournalFailsClosedOnUnknownEntries(t *testing.T) {
	store := newFileSaveJournalStore(t)
	root, _ := createFileSaveJournal(t, store)
	defer root.Close()
	journalDir := filepath.Join(store.root, ".stow", "file-saves")
	unknown := filepath.Join(journalDir, "identity.json.tmp")
	if err := os.WriteFile(unknown, []byte("temporary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("unknown temporary entry load=%v", err)
	}
	if err := os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceFileSaveJournalRejectsRecordsBeyondRemainingBudget(t *testing.T) {
	store := newFileSaveJournalStore(t)
	root, journal := createFileSaveJournal(t, store)
	defer root.Close()
	record := newFileSaveJournalRecord(store, journal, "payload")
	record.Phase = workspaceFileSaveNotCommitted
	data, err := encodeWorkspaceFileSaveRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.root, workspaceFileSaveReceiptsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, workspaceFileSaveReceiptsDir, workspaceFileSaveReceiptName(record.RequestKey))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	journal.bytes = workspaceFileSaveBytesLimit - 1
	if err := journal.loadReceipts(root); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("receipt exceeding remaining budget=%v", err)
	}
}

func TestWorkspaceFileSaveJournalLoadsAtExactByteCapacity(t *testing.T) {
	store := newFileSaveJournalStore(t)
	root, journal := createFileSaveJournal(t, store)
	defer root.Close()
	receiptsPath := filepath.Join(store.root, workspaceFileSaveReceiptsDir)
	if err := os.MkdirAll(receiptsPath, 0o700); err != nil {
		t.Fatal(err)
	}
	identityInfo, err := os.Stat(filepath.Join(store.root, workspaceFileSaveIdentityPath))
	if err != nil {
		t.Fatal(err)
	}
	remaining := workspaceFileSaveBytesLimit - int(identityInfo.Size())
	for index := 0; index < workspaceFileSaveEntryLimit; index++ {
		record := newFileSaveJournalRecord(store, journal, "payload")
		record.RequestKey = fmt.Sprintf("request-%04d", index)
		record.Phase = workspaceFileSaveNotCommitted
		target := remaining / (workspaceFileSaveEntryLimit - index)
		data, err := workspaceFileSaveRecordWithEncodedSize(record, target)
		if err != nil {
			t.Fatalf("record %d target %d: %v", index, target, err)
		}
		if err := os.WriteFile(filepath.Join(receiptsPath, workspaceFileSaveReceiptName(record.RequestKey)), data, 0o600); err != nil {
			t.Fatal(err)
		}
		remaining -= len(data)
	}
	if remaining != 0 {
		t.Fatalf("journal did not reach exact capacity; remaining=%d", remaining)
	}
	loaded, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket)
	if err != nil || loaded.bytes != workspaceFileSaveBytesLimit || len(loaded.records) != workspaceFileSaveEntryLimit {
		t.Fatalf("exact-capacity journal bytes=%d entries=%d error=%v", loaded.bytes, len(loaded.records), err)
	}
	if _, ok := loaded.lookup("request-1023"); !ok {
		t.Fatal("exact-capacity journal hid a retained receipt")
	}
	if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), store.manifest.Bucket); err != nil {
		t.Fatalf("barrier refused exact-capacity terminal journal: %v", err)
	}
	pendingPath := filepath.Join(store.root, workspaceFileSavePendingPath)
	if err := os.WriteFile(pendingPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("pending record beyond exact capacity=%v", err)
	}
	if err := os.Remove(pendingPath); err != nil {
		t.Fatal(err)
	}
}

func workspaceFileSaveRecordWithEncodedSize(record workspaceFileSaveRecord, size int) ([]byte, error) {
	record.PlannedEntry.ContentType = "x"
	data, err := encodeWorkspaceFileSaveRecord(record)
	if err != nil || len(data) > size {
		return nil, storage.ErrSaveRequestFull
	}
	record.PlannedEntry.ContentType = strings.Repeat("x", 1+size-len(data))
	data, err = encodeWorkspaceFileSaveRecord(record)
	if err != nil || len(data) != size {
		return nil, storage.ErrSaveRequestFull
	}
	return data, nil
}

func assertWorkspaceFileSaveJournalRejectsInvalidIdentity(t *testing.T, store *Store, root *rooted.Root, identity workspaceFileSaveIdentity) {
	t.Helper()
	identity.Version = 2
	encoded, err := encodeWorkspaceFileSaveIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic(workspaceFileSaveIdentityPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("unsupported identity version=%v", err)
	}
	identity.Version = 1
	encoded, err = encodeWorkspaceFileSaveIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	encoded[len(encoded)-2] ^= 1
	if err := root.WriteAtomic(workspaceFileSaveIdentityPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("corrupt identity=%v", err)
	}
	other := newFileSaveJournalStore(t)
	otherRoot, err := other.readRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer otherRoot.Close()
	if err := otherRoot.WriteAtomic(workspaceFileSaveIdentityPath, encodeMustWorkspaceFileSaveIdentity(t, identity), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkspaceFileSaveJournal(otherRoot, store.ID(), store.manifest.Bucket); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("foreign root identity=%v", err)
	}
}

func readWorkspaceSaveJournalFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWorkspaceFileSaveJournalValidatesRecordsAndCapacity(t *testing.T) {
	store := newFileSaveJournalStore(t)
	root, journal := createFileSaveJournal(t, store)
	defer root.Close()
	record := newFileSaveJournalRecord(store, journal, "payload")
	if err := journal.reserve(record); err != nil {
		t.Fatal(err)
	}
	if journal.reserved <= 0 || journal.bytes+journal.reserved > workspaceFileSaveBytesLimit {
		t.Fatalf("reservation=%d bytes=%d", journal.reserved, journal.bytes)
	}
	tooLarge := record
	tooLarge.PlannedEntry.Metadata = map[string]string{"large": strings.Repeat("x", workspaceFileSaveRecordLimit)}
	if _, err := encodeWorkspaceFileSaveRecord(tooLarge); !errors.Is(err, storage.ErrSaveRequestFull) && !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("oversize record error=%v", err)
	}
	invalid := record
	invalid.Path = "../escape"
	if _, err := encodeWorkspaceFileSaveRecord(invalid); !errors.Is(err, storage.ErrInvalidSaveRequest) {
		t.Fatalf("unsafe path record error=%v", err)
	}
	full := *journal
	full.records = make(map[string]workspaceFileSaveRecord, workspaceFileSaveEntryLimit)
	for i := 0; i < workspaceFileSaveEntryLimit; i++ {
		full.records[fmt.Sprintf("request-%d", i)] = workspaceFileSaveRecord{RequestKey: fmt.Sprintf("request-%d", i)}
	}
	if err := full.reserve(record); !errors.Is(err, storage.ErrSaveRequestFull) {
		t.Fatalf("full journal reserve=%v", err)
	}
	terminal := record
	terminal.Phase = workspaceFileSaveNotCommitted
	byteFull := *journal
	byteFull.pending, byteFull.pendingReserve, byteFull.reserved = nil, 0, 0
	byteFull.records = map[string]workspaceFileSaveRecord{"retained": terminal}
	byteFull.bytes = workspaceFileSaveBytesLimit
	if retained, ok := byteFull.lookup("retained"); !ok || retained.Phase != workspaceFileSaveNotCommitted {
		t.Fatalf("full journal hid retained receipt=%+v found=%v", retained, ok)
	}
	if err := byteFull.reserve(record); !errors.Is(err, storage.ErrSaveRequestFull) {
		t.Fatalf("byte-full journal reserve=%v", err)
	}
	for _, key := range []string{"", " request", "request ", strings.Repeat("x", 257), string([]byte{0xff})} {
		if validWorkspaceFileSaveRequestKey(key) {
			t.Fatalf("invalid request key accepted: %q", key)
		}
	}
	if !validWorkspaceFileSaveRequestKey("clé-1") {
		t.Fatal("valid trimmed UTF-8 request key refused")
	}
}

func TestWorkspaceFileSaveJournalRejectsLossyTextAndInvalidTargetFields(t *testing.T) {
	store := newFileSaveJournalStore(t)
	root, journal := createFileSaveJournal(t, store)
	defer root.Close()
	base := newFileSaveJournalRecord(store, journal, "payload")
	mutations := []struct {
		name   string
		change func(*workspaceFileSaveRecord)
	}{
		{"invalid content type", func(record *workspaceFileSaveRecord) { record.PlannedEntry.ContentType = string([]byte{0xff}) }},
		{"invalid metadata key", func(record *workspaceFileSaveRecord) {
			record.PlannedEntry.Metadata = map[string]string{string([]byte{0xff}): "value"}
		}},
		{"invalid metadata value", func(record *workspaceFileSaveRecord) {
			record.PlannedEntry.Metadata = map[string]string{"key": string([]byte{0xff})}
		}},
		{"multipart target", func(record *workspaceFileSaveRecord) { record.PlannedEntry.Multipart = true }},
		{"target digest", func(record *workspaceFileSaveRecord) { record.PlannedEntry.Digest = strings.Repeat("a", 64) }},
		{"target content etag", func(record *workspaceFileSaveRecord) { record.PlannedEntry.ContentETag = "composite" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			record := base
			mutation.change(&record)
			if _, err := encodeWorkspaceFileSaveRecord(record); !errors.Is(err, storage.ErrInvalidSaveRequest) {
				t.Fatalf("invalid target accepted: %v", err)
			}
		})
	}
	base.PreFileAbsent, base.PreEntryIndexed = false, true
	base.PreEntry = &ManifestEntry{Form: FormNatural, Multipart: true, ContentETag: "prior-composite"}
	if _, err := encodeWorkspaceFileSaveRecord(base); err != nil {
		t.Fatalf("existing multipart pre-entry rejected: %v", err)
	}
	encoded, err := encodeWorkspaceFileSaveRecord(newFileSaveJournalRecord(store, journal, "payload"))
	if err != nil {
		t.Fatal(err)
	}
	bad := bytes.Replace(encoded, []byte("text/plain"), []byte{0xff, 'e', 'x', 't', '/', 'p', 'l', 'a', 'i', 'n'}, 1)
	if _, err := decodeWorkspaceFileSaveRecord(bad); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("invalid raw UTF-8 accepted: %v", err)
	}
}

func newFileSaveJournalStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createFileSaveJournal(t *testing.T, store *Store) (*rooted.Root, *workspaceFileSaveJournal) {
	t.Helper()
	root, err := store.readRoot()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := initializeWorkspaceFileSaveJournal(root, store.ID(), store.manifest.Bucket)
	if err != nil {
		root.Close()
		t.Fatal(err)
	}
	return root, journal
}

func newFileSaveJournalRecord(store *Store, journal *workspaceFileSaveJournal, payload string) workspaceFileSaveRecord {
	payloadDigest := sha256.Sum256([]byte(payload))
	meaning := sha256.Sum256([]byte("meaning"))
	basis := sha256.Sum256([]byte("opaque observation token"))
	version, _ := storage.NewRecordVersion()
	return workspaceFileSaveRecord{Version: 1, WorkspaceID: store.ID(), Bucket: store.manifest.Bucket, JournalID: journal.identity.JournalID,
		RequestKey: "request-key", Path: "note.txt", Meaning: hex.EncodeToString(meaning[:]), Phase: workspaceFileSavePrepared, BasisTokenDigest: hex.EncodeToString(basis[:]),
		PreFileAbsent: true, PlannedEntry: ManifestEntry{Form: FormNatural, Size: int64(len(payload)), ETag: storage.ETagForBytes([]byte(payload)), VersionID: version, ContentType: "text/plain"},
		PayloadDigest: hex.EncodeToString(payloadDigest[:]), PayloadSize: int64(len(payload)), Mode: 0o644}
}

func testWorkspaceFileSaveProof(t *testing.T, root *rooted.Root, digest string, size int64, mode uint32) workspaceFileSaveProof {
	t.Helper()
	rootDevice, rootInode, ok := fileSaveNativeIdentity(root.Info())
	if !ok {
		t.Skip("native root identity is unavailable")
	}
	parent, err := root.OpenDirectory(".")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	parentInfo, err := parent.Stat()
	if err != nil {
		t.Fatal(err)
	}
	parentDevice, parentInode, ok := fileSaveNativeIdentity(parentInfo)
	if !ok {
		t.Skip("native parent identity is unavailable")
	}
	filePath := filepath.Join(t.TempDir(), "proof-file")
	if err := os.WriteFile(filePath, []byte("proof"), os.FileMode(mode)); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	fileDevice, fileInode, ok := fileSaveNativeIdentity(fileInfo)
	if !ok {
		t.Skip("native file identity is unavailable")
	}
	return workspaceFileSaveProof{RootDevice: rootDevice, RootInode: rootInode, ParentDevice: parentDevice, ParentInode: parentInode,
		FileDevice: fileDevice, FileInode: fileInode, ModifiedNS: fileInfo.ModTime().UnixNano(), Digest: digest, Size: size, Mode: mode}
}

func encodeMustWorkspaceFileSaveIdentity(t *testing.T, identity workspaceFileSaveIdentity) []byte {
	t.Helper()
	encoded, err := encodeWorkspaceFileSaveIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
