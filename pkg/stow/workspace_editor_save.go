package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type workspaceFileSaveRequest struct {
	Path             string
	Data             []byte
	RequestKey       string
	ObservationToken string
	Replace          bool
}

func (e *WorkspaceEditor) saveFile(ctx context.Context, request workspaceFileSaveRequest) (storage.SaveReceipt, error) {
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
	if err := validateWorkspaceEditorSaveRequest(request, e); err != nil {
		return storage.SaveReceipt{}, err
	}
	lock, err := workspace.AcquireMutationCapture(w.registryDir, w.id)
	if err != nil {
		return storage.SaveReceipt{}, ErrWorkspaceEditorUnavailable
	}
	defer lock.Release()
	save := workspaceEditorStorageRequest(request, e.maxBytes)
	receipt, found, err := w.instance.ReplayWorkspaceFileSave(ctx, w.bucket, save)
	if found || err != nil {
		return receipt, err
	}
	if err := w.store.CheckFileSaveMarker(); err != nil {
		return storage.SaveReceipt{}, ErrWorkspaceEditorUnavailable
	}
	if !request.Replace {
		basis, err := e.workspaceFilePublishBasis(workspaceFilePublishRequest{Path: request.Path, Data: request.Data, ObservationToken: request.ObservationToken})
		if err != nil {
			return storage.SaveReceipt{}, err
		}
		save.Write.Basis = basis
	}
	return w.instance.SaveWorkspaceFile(ctx, w.bucket, save)
}

func validateWorkspaceEditorSaveRequest(request workspaceFileSaveRequest, editor *WorkspaceEditor) error {
	if !validWorkspaceReadPath(request.Path, false) || !validWorkspaceEditorRequestKey(request.RequestKey) ||
		request.Replace == (request.ObservationToken != "") || int64(len(request.Data)) > editor.maxBytes {
		return storage.ErrInvalidSaveRequest
	}
	if workspaceReadExcluded(request.Path) || !workspaceReadFileAllowed(request.Path, editor.writeRoots) {
		return ErrWorkspaceEditorUnavailable
	}
	return nil
}

func validWorkspaceEditorRequestKey(key string) bool {
	return key != "" && len(key) <= 256 && strings.TrimSpace(key) == key && utf8.ValidString(key)
}

func workspaceEditorStorageRequest(request workspaceFileSaveRequest, maxBytes int64) storage.WorkspaceFileSaveRequest {
	var tokenDigest string
	if !request.Replace {
		digest := sha256.Sum256([]byte(request.ObservationToken))
		tokenDigest = hex.EncodeToString(digest[:])
	}
	return storage.WorkspaceFileSaveRequest{RequestKey: request.RequestKey, BasisTokenDigest: tokenDigest,
		Write: storage.WorkspaceFileWriteRequest{Path: request.Path, Data: append([]byte(nil), request.Data...), Replace: request.Replace, MaxFileBytes: maxBytes}}
}
