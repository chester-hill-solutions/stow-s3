package stow

import (
	"context"
	"errors"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

var (
	ErrRecoveryHoldsUnsupported = storage.ErrRecoveryHoldsUnsupported
	ErrInvalidRecoveryHold      = storage.ErrInvalidRecoveryHold
)

// mapRecoveryHoldError reuses the vocabulary the registry already publishes, so
// a caller does not have to know whether the hold it took was a checkpoint
// dependency or an object one to learn that the hold is gone, or that the
// journal is full.
func mapRecoveryHoldError(err error) error {
	switch {
	case errors.Is(err, storage.ErrRecoveryHoldNotFound):
		return ErrRecoveryHoldNotFound
	case errors.Is(err, storage.ErrRecoveryHoldFull):
		return ErrRecoveryHoldFull
	default:
		return err
	}
}

type ObjectRecoveryReference struct {
	Bucket, Key string
	Condition   SaveCondition
}

type ObjectRecoveryHoldOptions struct {
	ID, Owner string
	Objects   []ObjectRecoveryReference
}

func (r *Runtime) SupportsRecoveryHolds() bool { return r.inner.SupportsRecoveryHolds() }

// BeginObjectRecoveryHold protects observed bytes by refusing their mutation until release.
func (r *Runtime) BeginObjectRecoveryHold(ctx context.Context, options ObjectRecoveryHoldOptions) error {
	if len(options.Objects) == 0 || len(options.Objects) > 32 {
		return ErrInvalidRecoveryHold
	}
	refs := make([]stowruntime.ObjectRecoveryReference, 0, len(options.Objects))
	for _, ref := range options.Objects {
		refs = append(refs, stowruntime.ObjectRecoveryReference{Bucket: ref.Bucket, Key: ref.Key, Condition: ref.Condition.inner})
	}
	return mapRecoveryHoldError(mapSaveError(r.inner.BeginObjectRecoveryHold(ctx, stowruntime.ObjectRecoveryHoldOptions{ID: options.ID, Owner: options.Owner, Objects: refs})))
}

func (r *Runtime) ReleaseObjectRecoveryHold(ctx context.Context, id, owner string) error {
	return mapRecoveryHoldError(mapSaveError(r.inner.ReleaseObjectRecoveryHold(ctx, id, owner)))
}
