package runtime

import (
	"context"
	"math"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// RefreshUsage reconciles managed persistent records while excluding runtime mutations.
func (i *Instance) RefreshUsage(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return err
	}
	i.usageDirty = true
	return i.refreshPersistentUsageLocked(ctx)
}

func (i *Instance) refreshUsageObservationLocked(ctx context.Context) error {
	if i.options.Backend != BackendWorkspace && !(i.options.Backend == BackendFilesystem && i.saveRequestsLocked()) {
		return nil
	}
	if i.options.Backend == BackendFilesystem {
		if _, ok := i.store.(storage.UsageRevisionStore); ok {
			observation, err := observeStoredUsage(ctx, i.store)
			if err != nil {
				return err
			}
			if observation.revision == i.usageRevision && !i.usageDirty {
				i.recoveryErr = observation.blocked
				i.usageDirty = observation.blocked != nil
				return nil
			}
		}
	}
	options := i.options
	options.MaxBytes, options.MaxObjects = math.MaxInt64, math.MaxInt64
	snapshot, err := i.snapshotUsage(ctx, options)
	if err != nil {
		return err
	}
	i.usageState.adopt(snapshot)
	return nil
}

func (i *Instance) refreshPersistentUsageLocked(ctx context.Context) error {
	if err := i.refreshUsageObservationLocked(ctx); err != nil {
		return err
	}
	return i.recoveryErr
}

func (i *Instance) checkMutationLocked(ctx context.Context) error {
	if err := i.checkOpen(); err != nil {
		return err
	}
	return i.refreshPersistentUsageLocked(ctx)
}
