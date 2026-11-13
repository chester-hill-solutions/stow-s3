package runtime

import (
	"context"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *Instance) ObserveWorkspaceFile(ctx context.Context, bucket, path string, maxBytes int64) (storage.WorkspaceFileObservation, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return storage.WorkspaceFileObservation{}, err
	}
	if err := i.checkContext(ctx); err != nil {
		return storage.WorkspaceFileObservation{}, err
	}
	if i.policy != nil {
		return storage.WorkspaceFileObservation{}, storage.ErrWorkspaceFileUnsupported
	}
	observer, ok := i.store.(storage.WorkspaceFileReadStore)
	if !ok || observer.WorkspaceFileBucket() != bucket {
		return storage.WorkspaceFileObservation{}, storage.ErrWorkspaceFileUnsupported
	}
	if err := i.checkResource(authority.ObjectRead, policy.Object(bucket, path)); err != nil {
		return storage.WorkspaceFileObservation{}, err
	}
	return observer.ObserveWorkspaceFile(ctx, bucket, path, maxBytes)
}

func (i *Instance) CheckWorkspaceFileRead(bucket, path string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return err
	}
	if i.policy != nil {
		return storage.ErrWorkspaceFileUnsupported
	}
	observer, ok := i.store.(storage.WorkspaceFileReadStore)
	if !ok || observer.WorkspaceFileBucket() != bucket {
		return storage.ErrWorkspaceFileUnsupported
	}
	return i.checkResource(authority.ObjectRead, policy.Object(bucket, path))
}
