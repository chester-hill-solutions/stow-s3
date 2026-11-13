package stow

import (
	"context"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func (e *WorkspaceEditor) resolveSave(ctx context.Context, path, requestKey string) (storage.SaveReceipt, error) {
	w := e.owner
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if !e.validLocked() {
		return storage.SaveReceipt{}, ErrWorkspaceEditorClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return storage.SaveReceipt{}, err
	}
	if !validWorkspaceReadPath(path, false) || !validWorkspaceEditorRequestKey(requestKey) {
		return storage.SaveReceipt{}, storage.ErrInvalidSaveRequest
	}
	if workspaceReadExcluded(path) || !workspaceReadFileAllowed(path, e.readRoots) {
		return storage.SaveReceipt{}, ErrWorkspaceEditorUnavailable
	}
	lock, err := workspace.AcquireMutationCapture(w.registryDir, w.id)
	if err != nil {
		return storage.SaveReceipt{}, ErrWorkspaceEditorUnavailable
	}
	defer lock.Release()
	return w.instance.ResolveWorkspaceFileSave(ctx, w.bucket, path, requestKey)
}
