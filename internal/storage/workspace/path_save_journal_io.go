package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (j *workspaceFileSaveJournal) lookup(requestKey string) (workspaceFileSaveRecord, bool) {
	record, ok := j.records[requestKey]
	if ok {
		return record, true
	}
	if j.pending != nil && j.pending.RequestKey == requestKey {
		return *j.pending, true
	}
	return workspaceFileSaveRecord{}, false
}

func (j *workspaceFileSaveJournal) reserve(record workspaceFileSaveRecord) error {
	if j.pending != nil || j.reserved != 0 || len(j.records) >= workspaceFileSaveEntryLimit {
		return storage.ErrSaveRequestFull
	}
	if _, found := j.records[record.RequestKey]; found {
		return storage.ErrSaveRequestConflict
	}
	if err := j.validateRecordIdentity(record); err != nil || record.Phase != workspaceFileSavePrepared {
		return storage.ErrInvalidSaveRequest
	}
	reserve, err := workspaceFileSaveReservation(record)
	if err != nil {
		return err
	}
	if reserve > workspaceFileSaveBytesLimit-j.bytes-j.reserved {
		return storage.ErrSaveRequestFull
	}
	j.reserved = reserve
	j.pendingReserve = reserve
	return nil
}

func (j *workspaceFileSaveJournal) writePending(root *rooted.Root, record workspaceFileSaveRecord) error {
	if err := j.validatePendingWrite(record); err != nil {
		return err
	}
	if err := verifyWorkspaceFileSavePending(root, j.pending); err != nil {
		return err
	}
	data, err := encodeWorkspaceFileSaveRecord(record)
	if err != nil {
		return err
	}
	if len(data) > j.pendingReserve {
		return storage.ErrSaveRequestFull
	}
	if err := root.WriteAtomic(workspaceFileSavePendingPath, data, 0o600); err != nil {
		return uncertain(err)
	}
	if err := root.Check(); err != nil {
		return storage.ErrSaveRequestUnknown
	}
	j.bytes += len(data) - j.pendingBytes
	j.pendingBytes = len(data)
	j.reserved = j.pendingReserve - len(data)
	copy := record
	j.pending = &copy
	return nil
}

func (j *workspaceFileSaveJournal) validatePendingWrite(record workspaceFileSaveRecord) error {
	if err := j.validateRecordIdentity(record); err != nil || record.Phase != workspaceFileSavePrepared && record.Phase != workspaceFileSavePublishing && record.Phase != workspaceFileSavePublished {
		return storage.ErrInvalidSaveRequest
	}
	if j.pending == nil {
		if record.Phase != workspaceFileSavePrepared || j.reserved == 0 {
			return storage.ErrSaveRequestFull
		}
		return nil
	}
	if j.pending.RequestKey != record.RequestKey || !validWorkspaceFileSaveTransition(j.pending.Phase, record.Phase) || !sameWorkspaceFileSavePlan(*j.pending, record) {
		return storage.ErrSaveRequestConflict
	}
	if j.pending.Phase == workspaceFileSavePublished && !sameWorkspaceFileSavePublished(j.pending.Published, record.Published) {
		return storage.ErrSaveRequestConflict
	}
	return nil
}

func validWorkspaceFileSaveTransition(oldPhase, nextPhase string) bool {
	return oldPhase == workspaceFileSavePrepared && (nextPhase == workspaceFileSavePublishing || nextPhase == workspaceFileSaveNotCommitted) ||
		oldPhase == workspaceFileSavePublishing && nextPhase == workspaceFileSavePublished ||
		oldPhase == workspaceFileSavePublished && nextPhase == workspaceFileSavePublished
}

func (j *workspaceFileSaveJournal) writeReceipt(root *rooted.Root, record workspaceFileSaveRecord) error {
	if err := j.validateReceiptWrite(record); err != nil {
		return err
	}
	if err := verifyWorkspaceFileSavePending(root, j.pending); err != nil {
		return err
	}
	data, err := encodeWorkspaceFileSaveRecord(record)
	if err != nil {
		return err
	}
	if j.bytes > workspaceFileSaveBytesLimit-len(data) {
		return storage.ErrSaveRequestFull
	}
	path := filepath.ToSlash(filepath.Join(workspaceFileSaveReceiptsDir, workspaceFileSaveReceiptName(record.RequestKey)))
	if file, err := root.OpenRegularFile(path); err == nil {
		file.Close()
		return storage.ErrSaveRequestConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return storage.ErrSaveRequestUnknown
	}
	if err := root.WriteAtomic(path, data, 0o600); err != nil {
		return uncertain(err)
	}
	if err := syncWorkspaceFileSaveNamespace(root, filepath.Dir(path)); err != nil {
		return storage.ErrSaveRequestUnknown
	}
	if err := root.Check(); err != nil {
		return storage.ErrSaveRequestUnknown
	}
	j.bytes += len(data)
	j.reserved = 0
	copy := record
	j.records[record.RequestKey] = copy
	return nil
}

func (j *workspaceFileSaveJournal) validateReceiptWrite(record workspaceFileSaveRecord) error {
	if err := j.validateRecordIdentity(record); err != nil || record.Phase != workspaceFileSaveCommitted && record.Phase != workspaceFileSaveNotCommitted {
		return storage.ErrInvalidSaveRequest
	}
	if j.pending == nil || j.pending.RequestKey != record.RequestKey || len(j.records) >= workspaceFileSaveEntryLimit || !sameWorkspaceFileSavePlan(*j.pending, record) {
		return storage.ErrSaveRequestFull
	}
	if record.Phase == workspaceFileSaveCommitted && (j.pending.Phase != workspaceFileSavePublished || !sameWorkspaceFileSavePublished(j.pending.Published, record.Published)) {
		return storage.ErrSaveRequestUnknown
	}
	if record.Phase == workspaceFileSaveNotCommitted && j.pending.Phase != workspaceFileSavePrepared {
		return storage.ErrSaveRequestUnknown
	}
	if _, exists := j.records[record.RequestKey]; exists {
		return storage.ErrSaveRequestConflict
	}
	return nil
}

func (j *workspaceFileSaveJournal) clearPending(root *rooted.Root) error {
	receipt, found := j.records[j.pendingKey()]
	if !j.validPendingClear(receipt, found) {
		return storage.ErrSaveRequestUnknown
	}
	if err := verifyWorkspaceFileSavePending(root, j.pending); err != nil {
		return err
	}
	parent, err := root.OpenDirectory(filepath.ToSlash(filepath.Dir(workspaceFileSavePendingPath)))
	if err != nil {
		return storage.ErrSaveRequestUnknown
	}
	defer parent.Close()
	if err := root.Remove(workspaceFileSavePendingPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return uncertain(err)
	}
	if err := parent.Sync(); err != nil {
		return uncertain(err)
	}
	if err := root.Check(); err != nil {
		return storage.ErrSaveRequestUnknown
	}
	j.bytes -= j.pendingBytes
	j.pendingBytes = 0
	j.pending = nil
	j.pendingReserve = 0
	j.reserved = 0
	return nil
}

func (j *workspaceFileSaveJournal) validPendingClear(receipt workspaceFileSaveRecord, found bool) bool {
	if j.pending == nil || !found || !sameWorkspaceFileSavePlan(*j.pending, receipt) {
		return false
	}
	if receipt.Phase == workspaceFileSaveCommitted {
		return sameWorkspaceFileSavePublished(j.pending.Published, receipt.Published)
	}
	return receipt.Phase == workspaceFileSaveNotCommitted && j.pending.Phase == workspaceFileSavePrepared
}

func (j *workspaceFileSaveJournal) pendingKey() string {
	if j.pending == nil {
		return ""
	}
	return j.pending.RequestKey
}

func verifyWorkspaceFileSavePending(root *rooted.Root, expected *workspaceFileSaveRecord) error {
	data, err := readWorkspaceFileSaveFile(root, workspaceFileSavePendingPath)
	if expected == nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return storage.ErrSaveRequestUnknown
	}
	if err != nil {
		return storage.ErrSaveRequestUnknown
	}
	want, err := encodeWorkspaceFileSaveRecord(*expected)
	if err != nil || !bytes.Equal(data, want) {
		return storage.ErrSaveRequestUnknown
	}
	return nil
}

func (j *workspaceFileSaveJournal) validateRecordIdentity(record workspaceFileSaveRecord) error {
	if record.WorkspaceID != j.identity.WorkspaceID || record.Bucket != j.identity.Bucket || record.JournalID != j.identity.JournalID {
		return storage.ErrSaveRequestUnknown
	}
	if record.Published != nil && (record.Published.RootDevice != j.identity.RootDevice || record.Published.RootInode != j.identity.RootInode) {
		return storage.ErrSaveRequestUnknown
	}
	return validateWorkspaceFileSaveRecord(record, false)
}

func sameWorkspaceFileSavePlan(left, right workspaceFileSaveRecord) bool {
	return bytes.Equal(workspaceFileSavePlanBytes(left), workspaceFileSavePlanBytes(right))
}

func workspaceFileSavePlanBytes(record workspaceFileSaveRecord) []byte {
	record.Integrity, record.Phase = "", ""
	record.Published, record.Meta = nil, nil
	record.PlannedEntry.Modified = time.Time{}
	data, _ := json.Marshal(record)
	return data
}

func sameWorkspaceFileSavePublished(left, right *workspaceFileSaveProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func workspaceFileSavePendingMatchesReceipt(pending, receipt workspaceFileSaveRecord) bool {
	if !sameWorkspaceFileSavePlan(pending, receipt) {
		return false
	}
	if receipt.Phase == workspaceFileSaveCommitted {
		return pending.Phase == workspaceFileSavePublished && sameWorkspaceFileSavePublished(pending.Published, receipt.Published)
	}
	return receipt.Phase == workspaceFileSaveNotCommitted && pending.Phase == workspaceFileSavePrepared
}
