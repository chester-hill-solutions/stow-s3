package stow

import (
	"context"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// WorkspaceFileSaveRequest names one idempotent save in an attached workspace.
type WorkspaceFileSaveRequest struct {
	Path             string
	Data             []byte
	RequestKey       string
	ObservationToken string
	Replace          bool
}

// WorkspaceFileSaveResult reports the original receipt without returning file bytes.
// Generation preserves the receipt ID even if the file changes later.
type WorkspaceFileSaveResult struct {
	Object     Object
	Generation string
	Outcome    SaveOutcome
	Replayed   bool
}

// ResumeWorkspaceForFileRecovery opens an existing registered workspace for resolving receipts.
func ResumeWorkspaceForFileRecovery(options WorkspaceOptions, id string) (*Workspace, error) {
	workspace, err := resumeWorkspaceForFileRecovery(options, id)
	return workspace, mapWorkspaceFileSaveError(err)
}

// SaveFile publishes bytes against a prior observation or an explicit replacement.
func (e *WorkspaceEditor) SaveFile(ctx context.Context, request WorkspaceFileSaveRequest) (WorkspaceFileSaveResult, error) {
	receipt, err := e.saveFile(ctx, workspaceFileSaveRequest{Path: request.Path, Data: request.Data,
		RequestKey: request.RequestKey, ObservationToken: request.ObservationToken, Replace: request.Replace})
	result := workspaceFileSaveResult(receipt)
	if result.Outcome == "" {
		result.Outcome = SaveNotCommitted
	}
	return result, mapWorkspaceFileSaveError(err)
}

// ResolveSave returns a retained result. A missing receipt does not prove no effect.
func (e *WorkspaceEditor) ResolveSave(ctx context.Context, path, requestKey string) (WorkspaceFileSaveResult, error) {
	receipt, err := e.resolveSave(ctx, path, requestKey)
	result := workspaceFileSaveResult(receipt)
	if result.Outcome == "" {
		result.Outcome = SaveUnknown
	}
	return result, mapWorkspaceFileSaveError(err)
}

func workspaceFileSaveResult(receipt storage.SaveReceipt) WorkspaceFileSaveResult {
	result := WorkspaceFileSaveResult{Outcome: SaveOutcome(receipt.Outcome), Replayed: receipt.Replayed}
	if receipt.Meta == nil {
		return result
	}
	meta := receipt.Meta
	result.Generation = meta.VersionID
	result.Object = Object{Bucket: meta.Bucket, Key: meta.Key, Size: meta.Size, ETag: meta.ETag,
		ContentType: meta.ContentType, Metadata: storage.CloneMetadata(meta.Metadata), LastModified: meta.LastModified}
	return result
}

func mapWorkspaceFileSaveError(err error) error {
	if err == nil {
		return nil
	}
	for _, pair := range [][2]error{
		{storage.ErrWorkspaceFileConflict, ErrSaveConflict},
		{storage.ErrWorkspaceFileUnsupported, ErrGuardedSavesUnsupported},
		{storage.ErrWorkspaceFileBlocked, ErrSaveRequestUnknown},
		{storage.ErrSaveRequestsUnsupported, ErrSaveRequestsUnsupported},
		{storage.ErrInvalidSaveRequest, ErrInvalidSaveRequest},
		{storage.ErrSaveRequestConflict, ErrSaveRequestConflict},
		{storage.ErrSaveRequestNotFound, ErrSaveRequestNotFound},
		{storage.ErrSaveRequestNotCommitted, ErrSaveRequestNotCommitted},
		{storage.ErrSaveRequestUnknown, ErrSaveRequestUnknown},
		{storage.ErrSaveRequestFull, ErrSaveRequestFull},
		{storage.ErrInvalidKey, ErrInvalidSaveRequest},
		{runtime.ErrClosed, ErrClosed},
		{runtime.ErrQuotaExceeded, ErrQuotaExceeded},
	} {
		if errors.Is(err, pair[0]) {
			if errors.Is(err, ErrMutationCommitted) {
				return errors.Join(ErrMutationCommitted, pair[1])
			}
			return pair[1]
		}
	}
	var denied *authority.ErrNotAuthorized
	if errors.As(err, &denied) {
		return denied
	}
	if errors.Is(err, policy.ErrStalePolicy) {
		return policy.ErrStalePolicy
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, ErrWorkspaceEditorClosed) || errors.Is(err, ErrWorkspaceEditorUnavailable) || errors.Is(err, ErrWorkspaceObservationInvalid) {
		if errors.Is(err, ErrWorkspaceEditorClosed) {
			return ErrWorkspaceEditorClosed
		}
		if errors.Is(err, ErrWorkspaceObservationInvalid) {
			return ErrWorkspaceObservationInvalid
		}
		return ErrWorkspaceEditorUnavailable
	}
	if errors.Is(err, ErrMutationCommitted) {
		return errors.Join(ErrMutationCommitted, ErrWorkspaceEditorUnavailable)
	}
	return ErrWorkspaceEditorUnavailable
}
