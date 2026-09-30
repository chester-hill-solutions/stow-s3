package runtime

import (
	"context"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type ObjectRecoveryReference struct {
	Bucket, Key string
	Condition   SaveCondition
}

type ObjectRecoveryHoldOptions struct {
	ID, Owner string
	Objects   []ObjectRecoveryReference
}

func (i *Instance) SupportsRecoveryHolds() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	store, ok := i.store.(storage.RecoveryHoldStore)
	return ok && store.SupportsRecoveryHolds()
}

func (i *Instance) BeginObjectRecoveryHold(ctx context.Context, options ObjectRecoveryHoldOptions) error {
	if err := i.checkRecoveryAuthority(ctx); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMutationLocked(ctx); err != nil {
		return err
	}
	store, ok := i.store.(storage.RecoveryHoldStore)
	if !ok || !store.SupportsRecoveryHolds() {
		return storage.ErrRecoveryHoldsUnsupported
	}
	refs, err := i.recoveryReferences(options.Objects)
	if err != nil {
		return err
	}
	return store.BeginRecoveryHold(ctx, storage.RecoveryHoldOptions{ID: options.ID, Owner: options.Owner, Objects: refs})
}

func (i *Instance) recoveryReferences(refs []ObjectRecoveryReference) ([]storage.ObjectResource, error) {
	if len(refs) == 0 || len(refs) > 32 {
		return nil, storage.ErrInvalidRecoveryHold
	}
	resources := make([]storage.ObjectResource, 0, len(refs))
	for _, ref := range refs {
		condition := ref.Condition
		if condition.owner != i || condition.bucket != ref.Bucket || condition.key != ref.Key || condition.guard.Absent || condition.replace {
			return nil, ErrInvalidSaveCondition
		}
		resources = append(resources, storage.ObjectResource{Bucket: ref.Bucket, Key: ref.Key, Guard: condition.guard})
	}
	return resources, nil
}

func (i *Instance) ReleaseObjectRecoveryHold(ctx context.Context, id, owner string) error {
	if err := i.checkRecoveryAuthority(ctx); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMutationLocked(ctx); err != nil {
		return err
	}
	store, ok := i.store.(storage.RecoveryHoldStore)
	if !ok || !store.SupportsRecoveryHolds() {
		return storage.ErrRecoveryHoldsUnsupported
	}
	return store.ReleaseRecoveryHold(ctx, id, owner)
}

func (i *Instance) checkRecoveryAuthority(ctx context.Context) error {
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	if err := i.check(authority.ObjectRead); err != nil {
		return err
	}
	return i.check(authority.ObjectWrite)
}
