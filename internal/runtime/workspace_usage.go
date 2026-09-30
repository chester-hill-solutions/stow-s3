package runtime

import (
	"context"
	"math"
)

// RefreshUsage reconciles managed persistent records while excluding runtime mutations.
func (i *Instance) RefreshUsage(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return err
	}
	return i.refreshPersistentUsageLocked(ctx)
}

func (i *Instance) refreshPersistentUsageLocked(ctx context.Context) error {
	if i.options.Backend != BackendWorkspace && !(i.options.Backend == BackendFilesystem && i.saveRequestsLocked()) {
		return nil
	}
	options := i.options
	options.MaxBytes, options.MaxObjects = math.MaxInt64, math.MaxInt64
	snapshot := newInstance(options, i.store, nil, i.persistent)
	if err := snapshot.initialize(ctx); err != nil {
		return err
	}
	i.usage = snapshot.usage
	i.multipart = snapshot.multipart
	i.multipartTargets = snapshot.multipartTargets
	i.reservedTargets = snapshot.reservedTargets
	i.reservedObjects = snapshot.reservedObjects
	i.reservedBytes = snapshot.reservedBytes
	return nil
}

func (i *Instance) checkMutationLocked(ctx context.Context) error {
	if err := i.checkOpen(); err != nil {
		return err
	}
	return i.refreshPersistentUsageLocked(ctx)
}
