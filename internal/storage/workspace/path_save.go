package workspace

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *Store) UsageRevision(ctx context.Context) (uint64, error) {
	if ctx != nil && ctx.Err() != nil {
		return 0, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.fileSaveBlocked || s.fileSaveJournalBlocked {
		return s.fileSaveUsageRevision, storage.ErrSaveRequestUnknown
	}
	return s.fileSaveUsageRevision, nil
}

func (s *Store) markWorkspaceFileBlockedLocked() {
	if s.fileSaveBlocked {
		return
	}
	s.fileSaveBlocked = true
	s.fileSaveUsageRevision++
}

func (s *Store) PublishWorkspaceFile(ctx context.Context, bucket string, request storage.WorkspaceFileWriteRequest) (storage.WorkspaceFilePublication, error) {
	publication := storage.WorkspaceFilePublication{Outcome: storage.WorkspaceFileNotCommitted}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return publication, ErrClosed
	}
	if err := s.checkFileSaveMarkerLocked(); err != nil {
		return publication, err
	}
	if s.fileSaveJournal != nil {
		return publication, storage.ErrWorkspaceFileBlocked
	}
	if err := s.validateWorkspaceFileWrite(bucket, request); err != nil {
		return publication, err
	}
	if err := ctx.Err(); err != nil {
		return publication, err
	}
	current, err := s.observeWorkspaceFileLocked(ctx, bucket, request.Path, request.MaxFileBytes)
	if err != nil {
		return publication, err
	}
	if request.Basis != nil {
		if err := storage.CheckWorkspaceFileBasis(*request.Basis, current); err != nil {
			return publication, err
		}
	}
	return s.publishWorkspaceFileLocked(ctx, bucket, request, current)
}

func (s *Store) validateWorkspaceFileWrite(bucket string, request storage.WorkspaceFileWriteRequest) error {
	if bucket != s.manifest.Bucket || !s.layout.IsNatural(request.Path) || storage.ValidateKey(request.Path) != nil {
		return storage.ErrInvalidKey
	}
	if existing, ok := s.folded[foldKey(bucket, request.Path)]; ok && existing != request.Path {
		return storage.ErrInvalidKey
	}
	if request.Replace == (request.Basis != nil) || (request.Basis != nil && len(request.Basis.Data) != 0) {
		return storage.ErrInvalidKey
	}
	if request.MaxFileBytes < 1 || request.MaxFileBytes > 64<<10 || int64(len(request.Data)) > request.MaxFileBytes {
		return storage.ErrWorkspaceFileTooLarge
	}
	return nil
}

func (s *Store) publishWorkspaceFileLocked(ctx context.Context, bucket string, request storage.WorkspaceFileWriteRequest, current storage.WorkspaceFileObservation) (storage.WorkspaceFilePublication, error) {
	publication := storage.WorkspaceFilePublication{Outcome: storage.WorkspaceFileNotCommitted}
	plan, err := s.planWorkspaceFileLocked(bucket, request, current)
	if err != nil {
		return publication, err
	}
	if err := ctx.Err(); err != nil {
		return publication, err
	}
	path := filepath.Join(s.root, filepath.FromSlash(request.Path))
	if err := s.writeWorkspaceFilePayload(path, plan.Prepared.Data, plan.Mode); err != nil {
		return s.workspaceFilePayloadWriteUnknown(err)
	}
	info, err := s.statPublishedWorkspaceFile(request.Path)
	if err != nil {
		return s.blockCommittedWorkspaceFile(publication, err)
	}
	newEntry := plan.Entry
	newEntry.Modified = info.ModTime().UTC().Truncate(time.Second)
	s.folded[foldKey(bucket, request.Path)] = request.Path
	if err := s.recordLocked(bucket, request.Path, newEntry); err != nil {
		return s.blockCommittedWorkspaceFile(publication, err)
	}
	publication.Outcome, publication.IndexReady = storage.WorkspaceFileCommitted, true
	publication.Meta = s.metaFromEntry(bucket, request.Path, newEntry, info)
	return publication, nil
}

type workspaceFileSavePlan struct {
	Prepared storage.PreparedPut
	Entry    ManifestEntry
	Mode     os.FileMode
}

func (s *Store) planWorkspaceFileLocked(bucket string, request storage.WorkspaceFileWriteRequest, current storage.WorkspaceFileObservation) (workspaceFileSavePlan, error) {
	entry, indexed := s.objectIndex.entry(bucket, request.Path)
	options := storage.PutOptions{}
	mode := os.FileMode(0644)
	if !current.Absent && indexed {
		options.ContentType = entry.ContentType
		options.Metadata = storage.CloneMetadata(entry.Metadata)
		if entry.ChecksumAlgorithm != "" {
			options.ComputeChecksum = true
			options.ChecksumAlgorithm = entry.ChecksumAlgorithm
		}
		mode = os.FileMode(current.Mode)
	} else {
		options.ContentType = detectWorkspaceFileContentType(request.Data)
		if !current.Absent {
			mode = os.FileMode(current.Mode)
		}
	}
	prepared, err := storage.PreparePut(bytes.NewReader(request.Data), options)
	if err != nil {
		return workspaceFileSavePlan{}, err
	}
	version, err := storage.NewRecordVersion()
	if err != nil {
		return workspaceFileSavePlan{}, err
	}
	planned := ManifestEntry{Form: FormNatural, Size: int64(len(prepared.Data)), ETag: prepared.ETag, VersionID: version,
		ContentType: prepared.Options.ContentType, Metadata: storage.CloneMetadata(prepared.Options.Metadata),
		ChecksumAlgorithm: prepared.Options.ChecksumAlgorithm, ChecksumValue: prepared.Options.ChecksumValue}
	return workspaceFileSavePlan{Prepared: prepared, Entry: planned, Mode: mode}, nil
}

func (s *Store) writeWorkspaceFilePayload(path string, data []byte, mode os.FileMode) error {
	if s.fileSavePayloadWriter != nil {
		return s.fileSavePayloadWriter(path, data, mode)
	}
	return s.writeAtomicMode(path, data, mode, false)
}

func (s *Store) workspaceFilePayloadWriteUnknown(writeErr error) (storage.WorkspaceFilePublication, error) {
	s.markWorkspaceFileBlockedLocked()
	return storage.WorkspaceFilePublication{Outcome: storage.WorkspaceFileUnknown},
		fmt.Errorf("%w: workspace file publication effect is unknown: %v", storage.ErrSaveRequestUnknown, writeErr)
}

func (s *Store) statPublishedWorkspaceFile(path string) (os.FileInfo, error) {
	fullPath := filepath.Join(s.root, filepath.FromSlash(path))
	file, err := s.openRead(fullPath, nil)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}

func (s *Store) blockCommittedWorkspaceFile(publication storage.WorkspaceFilePublication, cause error) (storage.WorkspaceFilePublication, error) {
	s.markWorkspaceFileBlockedLocked()
	publication.Outcome = storage.WorkspaceFileCommitted
	return publication, storage.CommittedError(cause)
}

func detectWorkspaceFileContentType(data []byte) string {
	return http.DetectContentType(data[:min(len(data), 512)])
}
