package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type usageObservation struct {
	revision uint64
	blocked  error
}

func observeStoredUsage(ctx context.Context, store storage.Store) (usageObservation, error) {
	tracker, ok := store.(storage.UsageRevisionStore)
	if !ok {
		return usageObservation{}, nil
	}
	revision, err := tracker.UsageRevision(ctx)
	if errors.Is(err, storage.ErrSaveRequestUnknown) {
		return usageObservation{revision: revision, blocked: err}, nil
	}
	return usageObservation{revision: revision}, err
}

func (i *Instance) snapshotUsage(ctx context.Context, options Options) (usageState, error) {
	if err := i.checkContext(ctx); err != nil {
		return usageState{}, err
	}
	before, err := observeStoredUsage(ctx, i.store)
	if err != nil {
		return usageState{}, err
	}
	snapshot, err := scanStoredUsage(ctx, options, i.store, i.quotaStore())
	if err != nil {
		return usageState{}, err
	}
	after, err := observeStoredUsage(ctx, i.store)
	if err != nil {
		return usageState{}, err
	}
	if before.revision != after.revision {
		return usageState{}, fmt.Errorf("usage changed during reconciliation")
	}
	snapshot.usageRevision = after.revision
	snapshot.recoveryErr = after.blocked
	snapshot.usageDirty = after.blocked != nil
	return snapshot, nil
}

func (i *Instance) initialize(ctx context.Context) error {
	snapshot, err := i.snapshotUsage(ctx, i.options)
	if err == nil {
		i.usageState = snapshot
	}
	return err
}
