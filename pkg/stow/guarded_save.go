package stow

import (
	"context"
	"errors"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

var (
	ErrGuardedSavesUnsupported = errors.New("stow: guarded saves unsupported")
	ErrInvalidSaveCondition    = errors.New("stow: invalid guarded save condition")
	ErrSaveConflict            = errors.New("stow: guarded save conflicts with current object")
)

type SaveOutcome = stowruntime.SaveOutcome

const (
	SaveCommitted    = stowruntime.SaveCommitted
	SaveNotCommitted = stowruntime.SaveNotCommitted
	SaveUnknown      = stowruntime.SaveUnknown
)

// SaveCondition is opaque and valid only for its issuing runtime and resource.
type SaveCondition struct{ inner stowruntime.SaveCondition }

func (c SaveCondition) ObservedAbsence() bool { return c.inner.ObservedAbsence() }

// ReplacementCondition deliberately bypasses comparison, while retaining authorization and quotas.
func ReplacementCondition() SaveCondition {
	return SaveCondition{inner: stowruntime.ReplacementCondition()}
}

type SaveResult struct {
	Object   Object
	Outcome  SaveOutcome
	Replayed bool
}

type SaveOptions struct {
	Condition  SaveCondition
	PutOptions PutOptions
	RequestKey string
}

func (r *Runtime) SupportsGuardedSaves() bool { return r.inner.SupportsGuardedSaves() }

// ReadForSave observes a complete object or absence. Its condition is not a permission.
func (r *Runtime) ReadForSave(ctx context.Context, bucket, key string) (Object, SaveCondition, error) {
	object, condition, err := r.inner.ReadForSave(ctx, bucket, key)
	return objectOf(object), SaveCondition{inner: condition}, mapSaveError(err)
}

// SaveObject compares at publication; RequestKey retains a receipt on qualified stores.
func (r *Runtime) SaveObject(ctx context.Context, bucket, key string, data []byte, options SaveOptions) (SaveResult, error) {
	result, err := r.inner.SaveObject(ctx, bucket, key, data, stowruntime.SaveOptions{
		Condition:  options.Condition.inner,
		RequestKey: options.RequestKey,
		PutOptions: stowruntime.PutOptions{
			ContentType: options.PutOptions.ContentType, Metadata: storage.CloneMetadata(options.PutOptions.Metadata),
			IfMatch: options.PutOptions.IfMatch, IfNoneMatch: options.PutOptions.IfNoneMatch,
		},
	})
	return SaveResult{Object: objectOf(result.Object), Outcome: result.Outcome, Replayed: result.Replayed}, mapSaveError(err)
}

func mapSaveError(err error) error {
	switch {
	case errors.Is(err, stowruntime.ErrGuardedSavesUnsupported):
		return ErrGuardedSavesUnsupported
	case errors.Is(err, stowruntime.ErrInvalidSaveCondition):
		return ErrInvalidSaveCondition
	case errors.Is(err, storage.ErrSaveConflict):
		return ErrSaveConflict
	default:
		return mapSaveRequestError(err)
	}
}
