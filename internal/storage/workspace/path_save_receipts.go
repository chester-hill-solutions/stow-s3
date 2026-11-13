package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const workspaceFileSaveMeaningPolicy = "preserve-indexed-metadata-mode-checksum-v1"

var _ storage.WorkspaceFileSaveStore = (*Store)(nil)

type workspaceFileSaveMeaning struct {
	Version          int    `json:"version"`
	WorkspaceID      string `json:"workspace_id"`
	JournalID        string `json:"journal_id"`
	Bucket           string `json:"bucket"`
	Path             string `json:"path"`
	RequestKey       string `json:"request_key"`
	Operation        string `json:"operation"`
	Replace          bool   `json:"replace"`
	BasisTokenDigest string `json:"basis_token_digest"`
	PayloadDigest    string `json:"payload_digest"`
	PayloadSize      int64  `json:"payload_size"`
	Preservation     string `json:"preservation"`
}

func (s *Store) workspaceFileSaveMeaning(bucket string, request storage.WorkspaceFileSaveRequest, journal *workspaceFileSaveJournal) (string, error) {
	if !validWorkspaceFileSaveRequest(bucket, request) || journal == nil {
		return "", storage.ErrInvalidSaveRequest
	}
	payload := sha256.Sum256(request.Write.Data)
	meaning := workspaceFileSaveMeaning{Version: 1, WorkspaceID: s.manifest.WorkspaceID, JournalID: journal.identity.JournalID,
		Bucket: bucket, Path: request.Write.Path, RequestKey: request.RequestKey, Operation: "save-file-v1", Replace: request.Write.Replace,
		BasisTokenDigest: request.BasisTokenDigest, PayloadDigest: hex.EncodeToString(payload[:]), PayloadSize: int64(len(request.Write.Data)), Preservation: workspaceFileSaveMeaningPolicy}
	data, err := json.Marshal(meaning)
	if err != nil {
		return "", storage.ErrInvalidSaveRequest
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func validWorkspaceFileSaveRequest(bucket string, request storage.WorkspaceFileSaveRequest) bool {
	write := request.Write
	if !validWorkspaceFileSaveRequestKey(request.RequestKey) || bucket == "" || storage.ValidateBucketName(bucket) != nil ||
		storage.ValidateKey(write.Path) != nil || write.MaxFileBytes < 1 || write.MaxFileBytes > 64<<10 || int64(len(write.Data)) > write.MaxFileBytes ||
		write.Replace && write.Basis != nil || write.Basis != nil && (write.Basis.Path != write.Path || len(write.Basis.Data) != 0 || write.Basis.Identity == nil) {
		return false
	}
	if write.Replace {
		return request.BasisTokenDigest == ""
	}
	return workspaceFileSaveDigest(request.BasisTokenDigest, 64)
}

func validFreshWorkspaceFileSaveRequest(request storage.WorkspaceFileSaveRequest) bool {
	return request.Write.Replace || request.Write.Basis != nil
}

func workspaceFileSaveReceipt(record workspaceFileSaveRecord) storage.SaveReceipt {
	var meta *storage.ObjectMeta
	if record.Meta != nil {
		copy := *record.Meta
		copy.Metadata = storage.CloneMetadata(copy.Metadata)
		meta = &copy
	}
	return storage.SaveReceipt{Meta: meta, Outcome: record.Phase, Replayed: true}
}

func cloneWorkspaceFileSaveReceipt(receipt storage.SaveReceipt) storage.SaveReceipt {
	if receipt.Meta != nil {
		copy := *receipt.Meta
		copy.Metadata = storage.CloneMetadata(copy.Metadata)
		receipt.Meta = &copy
	}
	return receipt
}

func (s *Store) markWorkspaceFileSaveJournalBlockedLocked() {
	if s.fileSaveJournalBlocked {
		return
	}
	s.fileSaveJournalBlocked = true
	s.fileSaveUsageRevision++
}

func (s *Store) clearWorkspaceFileSaveJournalBlockedLocked() {
	if !s.fileSaveJournalBlocked {
		return
	}
	s.fileSaveJournalBlocked = false
	s.fileSaveUsageRevision++
}

func (s *Store) ReplayWorkspaceFileSave(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.SaveReceipt, bool, error) {
	if err := workspaceFileSaveContextError(ctx); err != nil {
		return storage.SaveReceipt{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return storage.SaveReceipt{}, false, ErrClosed
	}
	if !validWorkspaceFileSaveRequest(bucket, request) || !s.layout.IsNatural(request.Write.Path) {
		return storage.SaveReceipt{}, false, storage.ErrInvalidSaveRequest
	}
	journal, err := s.reloadWorkspaceFileSaveJournalLocked()
	if errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		return storage.SaveReceipt{}, false, nil
	}
	if err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, false, err
	}
	return s.replayWorkspaceFileSaveLocked(ctx, bucket, request, journal)
}

func (s *Store) reloadWorkspaceFileSaveJournalLocked() (*workspaceFileSaveJournal, error) {
	journal, err := s.loadFileSaveJournal()
	if errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		if s.fileSaveJournal != nil {
			s.markWorkspaceFileSaveJournalBlockedLocked()
			return nil, storage.ErrSaveRequestUnknown
		}
		return nil, err
	}
	if err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		if errors.Is(err, storage.ErrWorkspaceFileUnsupported) {
			return nil, err
		}
		return nil, errors.Join(storage.ErrSaveRequestUnknown, err)
	}
	if s.fileSaveJournal == nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return nil, storage.ErrWorkspaceFileBlocked
	}
	if s.fileSaveJournal != nil && s.fileSaveJournal.identity.JournalID != journal.identity.JournalID {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return nil, storage.ErrSaveRequestUnknown
	}
	s.fileSaveJournal = journal
	if journal.pending != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
	} else {
		s.clearWorkspaceFileSaveJournalBlockedLocked()
	}
	return journal, nil
}

func (s *Store) replayWorkspaceFileSaveLocked(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest, journal *workspaceFileSaveJournal) (storage.SaveReceipt, bool, error) {
	meaning, err := s.workspaceFileSaveMeaning(bucket, request, journal)
	if err != nil {
		return storage.SaveReceipt{}, false, err
	}
	if terminal, found := journal.records[request.RequestKey]; found {
		return s.replayTerminalWorkspaceFileSaveLocked(terminal, meaning, journal)
	}
	if journal.pending != nil {
		if journal.pending.RequestKey == request.RequestKey {
			return s.resolvePendingReplayLocked(ctx, *journal.pending, meaning, journal)
		}
		if _, err := s.resolvePendingWorkspaceFileSaveLocked(context.Background(), journal); err != nil && !errors.Is(err, storage.ErrSaveRequestNotCommitted) {
			return storage.SaveReceipt{Outcome: "unknown"}, false, err
		}
		if terminal, found := journal.records[request.RequestKey]; found {
			return s.replayTerminalWorkspaceFileSaveLocked(terminal, meaning, journal)
		}
	}
	return storage.SaveReceipt{}, false, nil
}

func (s *Store) replayTerminalWorkspaceFileSaveLocked(record workspaceFileSaveRecord, meaning string, journal *workspaceFileSaveJournal) (storage.SaveReceipt, bool, error) {
	if record.Meaning != meaning {
		return storage.SaveReceipt{Outcome: "unknown"}, true, storage.ErrSaveRequestConflict
	}
	if journal.pending != nil && journal.pending.RequestKey == record.RequestKey {
		if err := s.clearWorkspaceFileSavePendingLocked(journal); err != nil {
			return recordWorkspaceFileSaveReceipt(record, true), true, workspaceFileSaveKnownReceiptError(record, err)
		}
	}
	return workspaceFileSaveReceipt(record), true, workspaceFileSaveReceiptError(record)
}

func recordWorkspaceFileSaveReceipt(record workspaceFileSaveRecord, replayed bool) storage.SaveReceipt {
	receipt := workspaceFileSaveReceipt(record)
	receipt.Replayed = replayed
	return receipt
}

func workspaceFileSaveKnownReceiptError(record workspaceFileSaveRecord, cause error) error {
	if record.Phase == workspaceFileSaveCommitted {
		return storage.CommittedError(cause)
	}
	return errors.Join(storage.ErrSaveRequestNotCommitted, cause)
}

func workspaceFileSaveReceiptError(record workspaceFileSaveRecord) error {
	if record.Phase == workspaceFileSaveNotCommitted {
		return storage.ErrSaveRequestNotCommitted
	}
	return nil
}

func (s *Store) resolvePendingReplayLocked(ctx context.Context, pending workspaceFileSaveRecord, meaning string, journal *workspaceFileSaveJournal) (storage.SaveReceipt, bool, error) {
	if pending.Meaning != meaning {
		return storage.SaveReceipt{Outcome: "unknown"}, true, storage.ErrSaveRequestConflict
	}
	receipt, err := s.resolvePendingWorkspaceFileSaveLocked(ctx, journal)
	if err != nil {
		return receipt, true, err
	}
	terminal, found := journal.records[pending.RequestKey]
	if !found {
		return storage.SaveReceipt{Outcome: "unknown"}, true, storage.ErrSaveRequestUnknown
	}
	return workspaceFileSaveReceipt(terminal), true, workspaceFileSaveReceiptError(terminal)
}

func workspaceFileSaveContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
