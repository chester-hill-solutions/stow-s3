package runtime

import (
	"context"
	"math"
)

// RefreshUsage reconciles direct workspace writes while excluding runtime mutations.
func (i *Instance) RefreshUsage(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return err
	}
	return i.refreshWorkspaceUsageLocked(ctx)
}

func (i *Instance) refreshWorkspaceUsageLocked(ctx context.Context) error {
	if i.options.Backend != BackendWorkspace {
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
	return i.refreshWorkspaceUsageLocked(ctx)
}
