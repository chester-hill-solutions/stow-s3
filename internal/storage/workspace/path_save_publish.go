package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *Store) SaveWorkspaceFile(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.SaveReceipt, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return storage.SaveReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return storage.SaveReceipt{}, ErrClosed
	}
	if bucket != s.manifest.Bucket || !validWorkspaceFileSaveRequest(bucket, request) || !s.layout.IsNatural(request.Write.Path) {
		return storage.SaveReceipt{}, storage.ErrInvalidSaveRequest
	}
	return s.saveWorkspaceFileLocked(ctx, bucket, request)
}

func (s *Store) saveWorkspaceFileLocked(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.SaveReceipt, error) {
	journal, err := s.reloadWorkspaceFileSaveJournalLocked()
	if errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		journal = nil
	} else if err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	if journal != nil {
		receipt, found, replayErr := s.replayWorkspaceFileSaveLocked(ctx, bucket, request, journal)
		if found || replayErr != nil {
			return receipt, replayErr
		}
		if journal.pending != nil {
			return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestUnknown
		}
	}
	if !validFreshWorkspaceFileSaveRequest(request) {
		return storage.SaveReceipt{}, storage.ErrInvalidSaveRequest
	}
	admission, err := s.prepareWorkspaceFileSaveLocked(ctx, bucket, request, journal)
	if err != nil {
		return storage.SaveReceipt{}, err
	}
	return s.runWorkspaceFileSaveLocked(ctx, bucket, request, admission)
}

type workspaceFileSaveAdmission struct {
	journal *workspaceFileSaveJournal
	record  workspaceFileSaveRecord
	plan    workspaceFileSavePlan
}

func (s *Store) prepareWorkspaceFileSaveLocked(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest, journal *workspaceFileSaveJournal) (workspaceFileSaveAdmission, error) {
	if s.fileSaveBlocked || s.fileSaveJournalBlocked {
		return workspaceFileSaveAdmission{}, storage.ErrWorkspaceFileBlocked
	}
	if err := s.validateWorkspaceFileWrite(bucket, request.Write); err != nil {
		return workspaceFileSaveAdmission{}, err
	}
	current, plan, err := s.observeAndPlanWorkspaceFileSave(ctx, bucket, request)
	if err != nil {
		return workspaceFileSaveAdmission{}, err
	}
	if err := ctx.Err(); err != nil {
		return workspaceFileSaveAdmission{}, err
	}
	if journal == nil {
		journal, err = s.initializeFileSaveJournal()
		if err != nil {
			s.markWorkspaceFileSaveJournalBlockedLocked()
			return workspaceFileSaveAdmission{}, err
		}
		s.fileSaveJournal = journal
	}
	meaning, err := s.workspaceFileSaveMeaning(bucket, request, journal)
	if err != nil {
		return workspaceFileSaveAdmission{}, err
	}
	record := s.newWorkspaceFileSaveRecord(request, meaning, current, plan)
	if err := journal.reserve(record); err != nil {
		return workspaceFileSaveAdmission{}, err
	}
	return workspaceFileSaveAdmission{journal: journal, record: record, plan: plan}, nil
}

func (s *Store) observeAndPlanWorkspaceFileSave(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.WorkspaceFileObservation, workspaceFileSavePlan, error) {
	current, err := s.observeWorkspaceFileLocked(ctx, bucket, request.Write.Path, request.Write.MaxFileBytes)
	if err != nil {
		return storage.WorkspaceFileObservation{}, workspaceFileSavePlan{}, err
	}
	if request.Write.Basis != nil {
		if err := storage.CheckWorkspaceFileBasis(*request.Write.Basis, current); err != nil {
			return storage.WorkspaceFileObservation{}, workspaceFileSavePlan{}, err
		}
	}
	plan, err := s.planWorkspaceFileLocked(bucket, request.Write, current)
	return current, plan, err
}

func (s *Store) runWorkspaceFileSaveLocked(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest, admission workspaceFileSaveAdmission) (storage.SaveReceipt, error) {
	journal, record := admission.journal, admission.record
	if err := s.writeWorkspaceFileSavePending(journal, record); err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	s.markWorkspaceFileSaveJournalBlockedLocked()
	if err := s.faultWorkspaceFileSavePhase("prepared"); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestUnknown
	}
	if err := ctx.Err(); err != nil {
		receipt, terminalErr := s.finishNotCommittedWorkspaceFileSaveLocked(journal, record)
		receipt.Replayed = false
		return receipt, errors.Join(err, terminalErr)
	}
	record.Phase = workspaceFileSavePublishing
	if err := s.writeWorkspaceFileSavePending(journal, record); err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	if err := s.faultWorkspaceFileSavePhase("publishing"); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestUnknown
	}
	return s.publishAdmittedWorkspaceFileSaveLocked(bucket, request, admission, record)
}

func (s *Store) publishAdmittedWorkspaceFileSaveLocked(bucket string, request storage.WorkspaceFileSaveRequest, admission workspaceFileSaveAdmission, record workspaceFileSaveRecord) (storage.SaveReceipt, error) {
	journal, plan := admission.journal, admission.plan
	path := filepath.Join(s.root, filepath.FromSlash(request.Write.Path))
	if err := s.writeWorkspaceFilePayload(path, plan.Prepared.Data, plan.Mode); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, fmt.Errorf("%w: workspace file publication effect is unknown: %v", storage.ErrSaveRequestUnknown, err)
	}
	proof, observed, err := s.proveWorkspaceFileSave(record)
	if err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	record.Phase, record.Published = workspaceFileSavePublished, &proof
	record.PlannedEntry.Modified = observed.Modified.UTC().Truncate(time.Second)
	if err := s.writeWorkspaceFileSavePending(journal, record); err != nil {
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	if err := s.faultWorkspaceFileSavePhase("published"); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestUnknown
	}
	if err := s.installPublishedWorkspaceFileSaveIndex(record); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	if err := s.faultWorkspaceFileSavePhase("indexed"); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, storage.ErrSaveRequestUnknown
	}
	meta := s.metaFromEntry(bucket, record.Path, record.PlannedEntry, workspaceFileSaveFileInfo(observed))
	meta.LastModified = observed.Modified.UTC()
	record.Phase, record.Meta = workspaceFileSaveCommitted, meta
	if err := s.writeWorkspaceFileSaveReceipt(journal, record); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return storage.SaveReceipt{Outcome: "unknown"}, err
	}
	receipt := cloneWorkspaceFileSaveReceipt(storage.SaveReceipt{Meta: meta, Outcome: workspaceFileSaveCommitted})
	if err := s.faultWorkspaceFileSavePhase("terminal"); err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return receipt, storage.CommittedError(storage.ErrSaveRequestUnknown)
	}
	if err := s.clearWorkspaceFileSavePendingLocked(journal); err != nil {
		return receipt, storage.CommittedError(err)
	}
	return receipt, nil
}

func (s *Store) newWorkspaceFileSaveRecord(request storage.WorkspaceFileSaveRequest, meaning string, current storage.WorkspaceFileObservation, plan workspaceFileSavePlan) workspaceFileSaveRecord {
	payloadDigest := sha256.Sum256(request.Write.Data)
	bucket := s.manifest.Bucket
	journal := s.fileSaveJournal
	preEntry, indexed := s.objectIndex.entry(bucket, request.Write.Path)
	var savedPreEntry *ManifestEntry
	if indexed {
		copy := preEntry
		savedPreEntry = &copy
	}
	return workspaceFileSaveRecord{Version: 1, WorkspaceID: s.manifest.WorkspaceID, Bucket: bucket, JournalID: journal.identity.JournalID,
		RequestKey: request.RequestKey, Path: request.Write.Path, Meaning: meaning, Phase: workspaceFileSavePrepared,
		BasisTokenDigest: request.BasisTokenDigest, Replace: request.Write.Replace, PreFileAbsent: current.Absent, PreEntryIndexed: indexed,
		PreEntry: savedPreEntry, PlannedEntry: plan.Entry, PayloadDigest: hex.EncodeToString(payloadDigest[:]), PayloadSize: int64(len(request.Write.Data)), Mode: uint32(plan.Mode.Perm())}
}

func (s *Store) writeWorkspaceFileSavePending(journal *workspaceFileSaveJournal, record workspaceFileSaveRecord) error {
	root, err := s.readRoot()
	if err == nil {
		err = journal.writePending(root, record)
		root.Close()
	}
	if err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return fmt.Errorf("%w: workspace save journal pending write: %v", storage.ErrSaveRequestUnknown, err)
	}
	s.fileSaveJournal = journal
	return nil
}

func (s *Store) faultWorkspaceFileSavePhase(phase string) error {
	if s.fileSavePhaseFault == nil {
		return nil
	}
	return s.fileSavePhaseFault(phase)
}

func (s *Store) writeWorkspaceFileSaveReceipt(journal *workspaceFileSaveJournal, record workspaceFileSaveRecord) error {
	root, err := s.readRoot()
	if err == nil {
		err = journal.writeReceipt(root, record)
		root.Close()
	}
	if err != nil {
		s.markWorkspaceFileSaveJournalBlockedLocked()
		return fmt.Errorf("%w: workspace save journal receipt write: %v", storage.ErrSaveRequestUnknown, err)
	}
	s.fileSaveJournal = journal
	return nil
}

func (s *Store) proveWorkspaceFileSave(record workspaceFileSaveRecord) (workspaceFileSaveProof, storage.WorkspaceFileObservation, error) {
	root, err := s.readRoot()
	if err != nil {
		return workspaceFileSaveProof{}, storage.WorkspaceFileObservation{}, storage.ErrSaveRequestUnknown
	}
	defer root.Close()
	observation, err := readWorkspacePath(context.Background(), root, pathReadRequest{key: record.Path, maxBytes: 64 << 10, entry: record.PlannedEntry, indexed: true})
	if err != nil || observation.Size != record.PayloadSize || observation.Digest != record.PayloadDigest || observation.Mode != record.Mode {
		return workspaceFileSaveProof{}, storage.WorkspaceFileObservation{}, storage.ErrSaveRequestUnknown
	}
	identity, ok := observation.Identity.(nativeWorkspaceFileIdentity)
	if !ok || identity.file == nil {
		return workspaceFileSaveProof{}, storage.WorkspaceFileObservation{}, storage.ErrSaveRequestUnknown
	}
	rootDevice, rootInode, rootOK := fileSaveNativeIdentity(identity.root)
	parentDevice, parentInode, parentOK := fileSaveNativeIdentity(identity.parent)
	fileDevice, fileInode, fileOK := fileSaveNativeIdentity(identity.file)
	if !rootOK || !parentOK || !fileOK || rootDevice != s.fileSaveJournal.identity.RootDevice || rootInode != s.fileSaveJournal.identity.RootInode {
		return workspaceFileSaveProof{}, storage.WorkspaceFileObservation{}, storage.ErrSaveRequestUnknown
	}
	proof := workspaceFileSaveProof{RootDevice: rootDevice, RootInode: rootInode, ParentDevice: parentDevice, ParentInode: parentInode,
		FileDevice: fileDevice, FileInode: fileInode, ModifiedNS: observation.Modified.UnixNano(), Digest: observation.Digest, Size: observation.Size, Mode: observation.Mode}
	return proof, observation, nil
}

func workspaceFileSaveFileInfo(observation storage.WorkspaceFileObservation) os.FileInfo {
	identity, _ := observation.Identity.(nativeWorkspaceFileIdentity)
	return identity.file
}
