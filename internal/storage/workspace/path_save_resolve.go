package workspace

import (
	"context"
	"errors"
	"reflect"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *Store) ResolveWorkspaceFileSave(ctx context.Context, bucket, path, requestKey string) (storage.SaveReceipt, error) {
	if err := workspaceFileSaveContextError(ctx); err != nil {
		return storage.SaveReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return storage.SaveReceipt{}, ErrClosed
	}
	if bucket != s.manifest.Bucket || !s.layout.IsNatural(path) || !validWorkspaceFileSaveRequestKey(requestKey) {
		return storage.SaveReceipt{}, storage.ErrInvalidSaveRequest
	}
	return s.resolveWorkspaceFileSaveLocked(ctx, bucket, path, requestKey)
}

func (s *Store) resolveWorkspaceFileSaveLocked(ctx context.Context, bucket, path, requestKey string) (storage.SaveReceipt, error) {
	journal, err := s.reloadWorkspaceFileSaveJournalLocked()
	if errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestNotFound
	}
	if err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	if terminal, found := journal.records[requestKey]; found {
		return s.replayResolvedWorkspaceFileSaveLocked(journal, terminal, bucket, path)
	}
	if journal.pending == nil {
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestNotFound
	}
	if journal.pending.RequestKey == requestKey && (journal.pending.Bucket != bucket || journal.pending.Path != path) {
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestConflict
	}
	if journal.pending.RequestKey == requestKey {
		return s.resolvePendingWorkspaceFileSaveLocked(ctx, journal)
	}
	if _, err := s.resolvePendingWorkspaceFileSaveLocked(ctx, journal); err != nil && !errors.Is(err, storage.ErrSaveRequestNotCommitted) {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestNotFound
}

func (s *Store) replayResolvedWorkspaceFileSaveLocked(journal *workspaceFileSaveJournal, terminal workspaceFileSaveRecord, bucket, path string) (storage.SaveReceipt, error) {
	if terminal.Bucket != bucket || terminal.Path != path {
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestConflict
	}
	if journal.pending != nil && journal.pending.RequestKey == terminal.RequestKey {
		if err := s.clearWorkspaceFileSavePendingLocked(journal); err != nil {
			return recordWorkspaceFileSaveReceipt(terminal, true), workspaceFileSaveKnownReceiptError(terminal, err)
		}
	}
	return workspaceFileSaveReceipt(terminal), workspaceFileSaveReceiptError(terminal)
}

func (s *Store) resolvePendingWorkspaceFileSaveLocked(ctx context.Context, journal *workspaceFileSaveJournal) (storage.SaveReceipt, error) {
	if journal.pending == nil {
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestNotFound
	}
	if err := workspaceFileSaveContextError(ctx); err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	record := *journal.pending
	switch record.Phase {
	case workspaceFileSavePrepared:
		return s.finishNotCommittedWorkspaceFileSaveLocked(journal, record)
	case workspaceFileSavePublishing:
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestUnknown
	case workspaceFileSavePublished:
		return s.finishPublishedWorkspaceFileSaveLocked(journal, record)
	default:
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestUnknown
	}
}

func (s *Store) finishNotCommittedWorkspaceFileSaveLocked(journal *workspaceFileSaveJournal, record workspaceFileSaveRecord) (storage.SaveReceipt, error) {
	record.Phase = workspaceFileSaveNotCommitted
	if err := s.writeWorkspaceFileSaveReceipt(journal, record); err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	receipt := workspaceFileSaveReceipt(record)
	if err := s.faultWorkspaceFileSavePhase("terminal"); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return receipt, errors.Join(storage.ErrSaveRequestNotCommitted, storage.ErrSaveRequestUnknown)
	}
	if err := s.clearWorkspaceFileSavePendingLocked(journal); err != nil {
		return receipt, workspaceFileSaveKnownReceiptError(record, err)
	}
	return receipt, storage.ErrSaveRequestNotCommitted
}

func (s *Store) finishPublishedWorkspaceFileSaveLocked(journal *workspaceFileSaveJournal, record workspaceFileSaveRecord) (storage.SaveReceipt, error) {
	proof, observed, err := s.proveWorkspaceFileSave(record)
	if err != nil || record.Published == nil || proof != *record.Published {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestUnknown
	}
	if err := s.installPublishedWorkspaceFileSaveIndex(record); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	meta := s.metaFromEntry(record.Bucket, record.Path, record.PlannedEntry, workspaceFileSaveFileInfo(observed))
	meta.LastModified = observed.Modified.UTC()
	record.Phase, record.Meta = workspaceFileSaveCommitted, meta
	if err := s.writeWorkspaceFileSaveReceipt(journal, record); err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	receipt := workspaceFileSaveReceipt(record)
	if err := s.faultWorkspaceFileSavePhase("terminal"); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return receipt, storage.CommittedError(storage.ErrSaveRequestUnknown)
	}
	if err := s.clearWorkspaceFileSavePendingLocked(journal); err != nil {
		return receipt, storage.CommittedError(err)
	}
	return receipt, nil
}

func (s *Store) installPublishedWorkspaceFileSaveIndex(record workspaceFileSaveRecord) error {
	current, indexed := s.objectIndex.entry(record.Bucket, record.Path)
	preEntry := record.PreEntry
	preMatches := indexed == record.PreEntryIndexed && (!indexed || sameWorkspaceFileSaveEntry(current, *preEntry))
	targetMatches := indexed && sameWorkspaceFileSaveEntry(current, record.PlannedEntry)
	if !preMatches && !targetMatches {
		return storage.ErrSaveRequestUnknown
	}
	if preMatches {
		s.objectIndex.setEntry(record.Bucket, record.Path, record.PlannedEntry)
		s.folded[foldKey(record.Bucket, record.Path)] = record.Path
	}
	if err := s.objectIndex.save(); err != nil {
		return storage.ErrSaveRequestUnknown
	}
	return nil
}

func sameWorkspaceFileSaveEntry(left, right ManifestEntry) bool {
	return reflect.DeepEqual(left, right)
}

func (s *Store) clearWorkspaceFileSavePendingLocked(journal *workspaceFileSaveJournal) error {
	if err := s.faultWorkspaceFileSavePhase("cleanup"); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.ErrSaveRequestUnknown
	}
	root, err := s.readRoot()
	if err == nil {
		err = journal.clearPending(root)
		closeErr := root.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.ErrSaveRequestUnknown
	}
	s.fileSaveJournal = journal
	s.clearWorkspaceFileSaveJournalBlockedLocked()
	return nil
}
