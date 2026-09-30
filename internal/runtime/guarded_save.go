package runtime

import (
	"context"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

var (
	ErrGuardedSavesUnsupported = errors.New("guarded saves unsupported")
	ErrInvalidSaveCondition    = errors.New("invalid guarded save condition")
)

type SaveOutcome string

const (
	SaveCommitted    SaveOutcome = "committed"
	SaveNotCommitted SaveOutcome = "not_committed"
	SaveUnknown      SaveOutcome = "unknown"
)

// SaveCondition is an observation bound to one instance and exact resource.
type SaveCondition struct {
	owner       *Instance
	bucket, key string
	guard       storage.WriteGuard
	replace     bool
}

func ReplacementCondition() SaveCondition { return SaveCondition{replace: true} }

func (c SaveCondition) ObservedAbsence() bool { return c.owner != nil && c.guard.Absent }

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

func (i *Instance) guardedSavesLocked() bool {
	store, ok := i.store.(storage.GuardedWriteStore)
	return ok && store.SupportsGuardedWrites()
}

func (i *Instance) SupportsGuardedSaves() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.guardedSavesLocked()
}

// ReadForSave returns an absence observation without treating a missing bucket as absence.
func (i *Instance) ReadForSave(ctx context.Context, bucket, key string) (Object, SaveCondition, error) {
	if err := i.checkContext(ctx); err != nil {
		return Object{}, SaveCondition{}, err
	}
	if err := i.checkResource(authority.ObjectRead, object(bucket, key)); err != nil {
		return Object{}, SaveCondition{}, err
	}
	if !i.SupportsGuardedSaves() {
		return Object{}, SaveCondition{}, ErrGuardedSavesUnsupported
	}
	object, err := i.GetObject(ctx, bucket, key)
	condition := SaveCondition{owner: i, bucket: bucket, key: key}
	if errors.Is(err, storage.ErrObjectNotFound) {
		condition.guard.Absent = true
		return Object{Bucket: bucket, Key: key}, condition, nil
	}
	if err != nil {
		return Object{}, SaveCondition{}, err
	}
	condition.guard.Fingerprint = storage.ObjectFingerprint(storage.ObjectMeta{
		VersionID: object.VersionID, ContentType: object.ContentType,
		Metadata: object.Metadata, ChecksumAlgorithm: object.ChecksumAlgorithm, ChecksumValue: object.ChecksumValue,
	}, object.Data)
	return object, condition, nil
}

func (i *Instance) SaveObject(ctx context.Context, bucket, key string, data []byte, options SaveOptions) (SaveResult, error) {
	refused := SaveResult{Outcome: SaveNotCommitted}
	if options.RequestKey != "" {
		refused.Outcome = SaveUnknown
	}
	if err := i.checkSaveAuthority(ctx, bucket, key, options.RequestKey); err != nil {
		return refused, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.validateSaveLocked(ctx, bucket, key, &options); err != nil {
		return refused, err
	}
	if options.RequestKey != "" {
		result, found, err := i.replaySaveLocked(ctx, bucket, key, data, options.PutOptions)
		if found || err != nil {
			return result, err
		}
	}
	if err := i.checkMutationLocked(ctx); err != nil {
		return refused, err
	}
	object, err := i.putObjectLocked(ctx, bucket, key, data, options.PutOptions)
	return SaveResult{Object: object, Outcome: saveOutcome(err)}, err
}

// checkSaveAuthority is the write side of a save, plus the read that a durable
// request adds: resolving a saved request discloses the receipt, so it is
// authorized on the same object the save would write.
func (i *Instance) checkSaveAuthority(ctx context.Context, bucket, key, requestKey string) error {
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	if err := i.checkResource(authority.ObjectWrite, object(bucket, key)); err != nil {
		return err
	}
	if requestKey != "" {
		return i.checkResource(authority.ObjectRead, object(bucket, key))
	}
	return nil
}

func (i *Instance) validateSaveLocked(ctx context.Context, bucket, key string, options *SaveOptions) error {
	if err := i.checkOpen(); err != nil {
		return err
	}
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	if options.RequestKey != "" && !i.saveRequestsLocked() {
		return storage.ErrSaveRequestsUnsupported
	}
	if !i.guardedSavesLocked() {
		return ErrGuardedSavesUnsupported
	}
	return i.bindSaveCondition(options, bucket, key)
}

func (i *Instance) bindSaveCondition(options *SaveOptions, bucket, key string) error {
	if options.PutOptions.IfMatch != "" || options.PutOptions.IfNoneMatch != "" {
		return ErrInvalidSaveCondition
	}
	condition := options.Condition
	if !condition.replace {
		if condition.owner != i || condition.bucket != bucket || condition.key != key {
			return ErrInvalidSaveCondition
		}
		options.PutOptions.guard = &condition.guard
	}
	options.PutOptions.requestKey = options.RequestKey
	return nil
}

func saveOutcome(err error) SaveOutcome {
	if err == nil || errors.Is(err, storage.ErrMutationCommitted) {
		return SaveCommitted
	}
	if errors.Is(err, storage.ErrSaveRequestUnknown) {
		return SaveUnknown
	}
	for _, refused := range []error{ErrQuotaExceeded, storage.ErrSaveConflict, storage.ErrBucketNotFound,
		storage.ErrInvalidBucketName, storage.ErrInvalidKey, storage.ErrPreconditionFailed,
		storage.ErrSaveRequestNotCommitted, storage.ErrSaveRequestFull,
		storage.ErrRecoveryHeld, storage.ErrInvalidRecoveryHold, storage.ErrRecoveryHoldNotFound,
		storage.ErrRecoveryHoldFull} {
		if errors.Is(err, refused) {
			return SaveNotCommitted
		}
	}
	return SaveUnknown
}
