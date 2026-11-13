package stow

import (
	"context"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type workspaceFilePublishRequest struct {
	Path             string
	Data             []byte
	ObservationToken string
	Replace          bool
}

func (e *WorkspaceEditor) publishFile(ctx context.Context, request workspaceFilePublishRequest) (storage.WorkspaceFilePublication, error) {
	refused := storage.WorkspaceFilePublication{Outcome: storage.WorkspaceFileNotCommitted}
	w := e.owner
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if !e.validLocked() {
		return refused, ErrWorkspaceEditorClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return refused, err
	}
	basis, err := e.workspaceFilePublishBasis(request)
	if err != nil {
		return refused, err
	}
	lock, err := workspace.AcquireMutationCapture(w.registryDir, w.id)
	if err != nil {
		return refused, ErrWorkspaceEditorUnavailable
	}
	defer lock.Release()
	if err := w.store.CheckFileSaveMarker(); err != nil {
		return refused, ErrWorkspaceEditorUnavailable
	}
	return w.instance.PublishWorkspaceFile(ctx, w.bucket, storage.WorkspaceFileWriteRequest{Path: request.Path, Data: append([]byte(nil), request.Data...),
		Basis: basis, Replace: request.Replace, MaxFileBytes: e.maxBytes})
}

func (e *WorkspaceEditor) workspaceFilePublishBasis(request workspaceFilePublishRequest) (*storage.WorkspaceFileObservation, error) {
	if !validWorkspaceReadPath(request.Path, false) || workspaceReadExcluded(request.Path) || !workspaceReadFileAllowed(request.Path, e.writeRoots) || int64(len(request.Data)) > e.maxBytes {
		return nil, ErrWorkspaceEditorUnavailable
	}
	if request.Replace == (request.ObservationToken != "") {
		return nil, ErrWorkspaceObservationInvalid
	}
	if request.Replace {
		return nil, nil
	}
	stored, ok := e.observations[request.ObservationToken]
	if !ok || stored.incarnation != e.incarnation || stored.path != request.Path || !e.owner.workspaceEditorNow().Before(stored.expires) {
		return nil, ErrWorkspaceObservationInvalid
	}
	return &storage.WorkspaceFileObservation{Path: stored.path, Absent: !stored.exists, Digest: stored.digest,
		Mode: stored.mode, Modified: stored.modified, Generation: stored.generation, Identity: stored.identity,
		Size: stored.size, Metadata: stored.metadata}, nil
}
