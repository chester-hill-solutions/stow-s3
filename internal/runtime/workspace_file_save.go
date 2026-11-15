package runtime

import (
	"context"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *Instance) PublishWorkspaceFile(ctx context.Context, bucket string, request storage.WorkspaceFileWriteRequest) (storage.WorkspaceFilePublication, error) {
	refused := storage.WorkspaceFilePublication{Outcome: storage.WorkspaceFileNotCommitted}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := i.checkContext(ctx); err != nil {
		return refused, err
	}
	if err := i.checkResource(authority.ObjectRead, policy.Object(bucket, request.Path)); err != nil {
		return refused, err
	}
	if err := i.checkResource(authority.ObjectWrite, policy.Object(bucket, request.Path)); err != nil {
		return refused, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.publishWorkspaceFileLocked(ctx, bucket, request)
}

func (i *Instance) publishWorkspaceFileLocked(ctx context.Context, bucket string, request storage.WorkspaceFileWriteRequest) (storage.WorkspaceFilePublication, error) {
	refused := storage.WorkspaceFilePublication{Outcome: storage.WorkspaceFileNotCommitted}
	writer, current, err := i.prepareWorkspaceFilePublication(ctx, bucket, request)
	if err != nil {
		return refused, err
	}
	if err := i.checkWorkspaceFileQuota(bucket, request.Path, request.Data, current); err != nil {
		return refused, err
	}
	publication, err := writer.PublishWorkspaceFile(ctx, bucket, request)
	if publication.Outcome == storage.WorkspaceFileCommitted {
		i.finishPut(objectTarget(bucket, request.Path), current.Size, int64(len(request.Data)), !current.Absent, err)
	} else if publication.Outcome == storage.WorkspaceFileUnknown {
		i.recordMutation(storage.ErrSaveRequestUnknown)
		i.recoveryErr = storage.ErrSaveRequestUnknown
	}
	return publication, err
}

func (i *Instance) prepareWorkspaceFilePublication(ctx context.Context, bucket string, request storage.WorkspaceFileWriteRequest) (storage.WorkspaceFileWriteStore, storage.WorkspaceFileObservation, error) {
	var empty storage.WorkspaceFileObservation
	if err := i.checkOpen(); err != nil {
		return nil, empty, err
	}
	if err := i.checkContext(ctx); err != nil {
		return nil, empty, err
	}
	if i.policy != nil {
		return nil, empty, storage.ErrWorkspaceFileUnsupported
	}
	reader, ok := i.store.(storage.WorkspaceFileReadStore)
	writer, writeOK := i.store.(storage.WorkspaceFileWriteStore)
	if !ok || !writeOK || reader.WorkspaceFileBucket() != bucket {
		return nil, empty, storage.ErrWorkspaceFileUnsupported
	}
	if err := i.checkMutationLocked(ctx); err != nil {
		return nil, empty, err
	}
	if err := validateRuntimeWorkspaceFileWrite(request); err != nil {
		return nil, empty, err
	}
	current, err := reader.ObserveWorkspaceFile(ctx, bucket, request.Path, request.MaxFileBytes)
	if err != nil {
		return nil, empty, err
	}
	if err := checkRuntimeWorkspaceFileBasis(request.Basis, current); err != nil {
		return nil, empty, err
	}
	return writer, current, nil
}

func (i *Instance) checkWorkspaceFileQuota(bucket, path string, data []byte, current storage.WorkspaceFileObservation) error {
	oldSize := current.Size
	if current.Absent {
		oldSize = 0
	}
	if !i.bytesQuotaFits(oldSize, int64(len(data))) {
		return ErrQuotaExceeded
	}
	target := objectTarget(bucket, path)
	if current.Absent && !i.objectQuotaFits(target, 1) {
		return ErrQuotaExceeded
	}
	return nil
}

func validateRuntimeWorkspaceFileWrite(request storage.WorkspaceFileWriteRequest) error {
	if request.Replace == (request.Basis != nil) || (request.Basis != nil && len(request.Basis.Data) != 0) {
		return storage.ErrInvalidKey
	}
	if request.MaxFileBytes < 1 || request.MaxFileBytes > 64<<10 || int64(len(request.Data)) > request.MaxFileBytes {
		return storage.ErrWorkspaceFileTooLarge
	}
	return nil
}

func checkRuntimeWorkspaceFileBasis(expected *storage.WorkspaceFileObservation, current storage.WorkspaceFileObservation) error {
	if expected == nil {
		return nil
	}
	return storage.CheckWorkspaceFileBasis(*expected, current)
}
