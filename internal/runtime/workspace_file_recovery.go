package runtime

import (
	"context"
	"math"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// OpenWithStoreForWorkspaceFileReceiptResolution opens a read-authorized recovery instance.
func OpenWithStoreForWorkspaceFileReceiptResolution(options Options, store storage.Store) (*Instance, error) {
	return openWithStore(options, store, nil, true)
}

func validateWorkspaceFileReceiptResolution(instance *Instance) error {
	if instance.options.Backend != BackendWorkspace || instance.policy != nil {
		return storage.ErrWorkspaceFileUnsupported
	}
	reader, ok := instance.store.(storage.WorkspaceFileReadStore)
	if !ok || reader.WorkspaceFileBucket() == "" || storage.ValidateBucketName(reader.WorkspaceFileBucket()) != nil {
		return storage.ErrWorkspaceFileUnsupported
	}
	if _, ok := instance.store.(storage.WorkspaceFileSaveStore); !ok {
		return storage.ErrWorkspaceFileUnsupported
	}
	if _, ok := instance.store.(storage.UsageRevisionStore); !ok {
		return storage.ErrWorkspaceFileUnsupported
	}
	return nil
}

func (i *Instance) initializeWorkspaceFileReceiptResolution(ctx context.Context) error {
	scanOptions := i.options
	scanOptions.MaxBytes = math.MaxInt64
	scanOptions.MaxObjects = math.MaxInt64
	scanOptions.MaxMultipartUploads = math.MaxInt64
	snapshot, err := i.snapshotUsage(ctx, scanOptions)
	if err != nil {
		return err
	}
	snapshot.limits = i.usageState.limits
	i.usageState = snapshot
	return nil
}
