package stow

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type WorkspaceFileRead struct {
	Path             string
	Data             []byte
	Absent           bool
	SHA256           string
	Mode             os.FileMode
	Modified         time.Time
	Generation       string
	ObservationToken string
	ExpiresAt        time.Time
}

func (e *WorkspaceEditor) ReadForSave(ctx context.Context, path string) (WorkspaceFileRead, error) {
	w := e.owner
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if !e.validLocked() {
		return WorkspaceFileRead{}, ErrWorkspaceEditorClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return WorkspaceFileRead{}, err
	}
	if !validWorkspaceReadPath(path, false) || workspaceReadExcluded(path) || !workspaceReadFileAllowed(path, e.readRoots) {
		return WorkspaceFileRead{}, ErrWorkspaceEditorUnavailable
	}
	lock, err := workspace.AcquireMutationCapture(w.registryDir, w.id)
	if err != nil {
		return WorkspaceFileRead{}, ErrWorkspaceEditorUnavailable
	}
	defer lock.Release()
	if err := w.store.CheckFileSaveMarker(); err != nil {
		return WorkspaceFileRead{}, ErrWorkspaceEditorUnavailable
	}
	observation, err := w.instance.ObserveWorkspaceFile(ctx, w.bucket, path, e.maxBytes)
	if err != nil {
		return WorkspaceFileRead{}, workspaceEditorReadError(err)
	}
	if err := ctx.Err(); err != nil {
		return WorkspaceFileRead{}, err
	}
	now := w.workspaceEditorNow()
	e.expireObservationsLocked(now)
	if len(e.observations) >= e.maxLive {
		return WorkspaceFileRead{}, ErrWorkspaceObservationFull
	}
	token, err := newWorkspaceEditorIncarnation()
	if err != nil {
		return WorkspaceFileRead{}, err
	}
	expires := now.Add(e.lifetime)
	basis := workspaceFileBasis{incarnation: e.incarnation, path: path, exists: !observation.Absent,
		digest: observation.Digest, size: observation.Size, mode: observation.Mode, modified: observation.Modified,
		generation: observation.Generation, identity: observation.Identity, metadata: observation.Metadata, expires: expires}
	e.observations[token] = basis
	return WorkspaceFileRead{Path: path, Data: append([]byte(nil), observation.Data...), Absent: observation.Absent,
		SHA256: observation.Digest, Mode: os.FileMode(observation.Mode), Modified: observation.Modified, Generation: observation.Generation,
		ObservationToken: token, ExpiresAt: expires}, nil
}

func (e *WorkspaceEditor) ReleaseObservation(token string) error {
	w := e.owner
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if !e.validLocked() {
		return ErrWorkspaceEditorClosed
	}
	e.expireObservationsLocked(w.workspaceEditorNow())
	if _, ok := e.observations[token]; !ok {
		return ErrWorkspaceObservationInvalid
	}
	delete(e.observations, token)
	return nil
}

func (e *WorkspaceEditor) expireObservationsLocked(now time.Time) {
	for token, basis := range e.observations {
		if !now.Before(basis.expires) {
			delete(e.observations, token)
		}
	}
}

func (w *Workspace) workspaceEditorNow() time.Time {
	if w.now != nil {
		return w.now().UTC()
	}
	return time.Now().UTC()
}

func workspaceEditorReadError(err error) error {
	if errors.Is(err, storage.ErrWorkspaceFileTooLarge) {
		return ErrWorkspaceReadTooLarge
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrWorkspaceEditorUnavailable
}
