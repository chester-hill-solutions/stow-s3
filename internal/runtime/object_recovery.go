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
	if err := i.checkRecoveryAuthority(ctx, options.Objects); err != nil {
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
	if err := i.checkRecoveryAuthority(ctx, nil); err != nil {
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

// checkRecoveryAuthority is checkResource for a hold over a set of objects.
//
// A hold needs both halves on every object it covers: the write, because a hold
// exists to stop those objects changing, and the read because naming them is a
// disclosure about which objects a caller believes are worth protecting.
//
// refs may be empty, which is what ReleaseObjectRecoveryHold passes. There is
// then no resource to match, so it falls back to the environment authority, which
// is also the pre-policy behaviour. Under a policy an unattributable hold is left
// alone rather than released: releasing is the irreversible direction, and an
// unattributable hold is not evidence the caller may release it.
func (i *Instance) checkRecoveryAuthority(ctx context.Context, refs []ObjectRecoveryReference) error {
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	if len(refs) == 0 {
		if err := i.check(authority.ObjectRead); err != nil {
			return err
		}
		return i.check(authority.ObjectWrite)
	}
	for _, ref := range refs {
		res := object(ref.Bucket, ref.Key)
		if err := i.checkResource(authority.ObjectRead, res); err != nil {
			return err
		}
		if err := i.checkResource(authority.ObjectWrite, res); err != nil {
			return err
		}
	}
	return nil
}
