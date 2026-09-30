package runtime

import (
	"context"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *Instance) saveRequestsLocked() bool {
	store, ok := i.store.(storage.SaveRequestStore)
	return ok && store.SupportsDurableSaveRequests()
}

func (i *Instance) SupportsDurableSaveRequests() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.saveRequestsLocked()
}

func (i *Instance) replaySaveLocked(ctx context.Context, bucket, key string, data []byte, options PutOptions) (SaveResult, bool, error) {
	receipt, found, err := i.store.(storage.SaveRequestStore).ReplaySaveRequest(ctx, bucket, key, data, storage.PutOptions{
		ContentType: options.ContentType, Metadata: storage.CloneMetadata(options.Metadata),
		ChecksumAlgorithm: options.ChecksumAlgorithm, ChecksumValue: options.ChecksumValue,
		Guard: options.guard, RequestKey: options.requestKey,
	})
	result := saveReceiptResult(receipt, err)
	if found {
		err = i.reconcileSaveReceiptLocked(ctx, result, err)
	}
	return result, found, err
}

func saveReceiptResult(receipt storage.SaveReceipt, err error) SaveResult {
	outcome := SaveOutcome(receipt.Outcome)
	if outcome == "" || errors.Is(err, storage.ErrSaveRequestConflict) || errors.Is(err, storage.ErrInvalidSaveRequest) || errors.Is(err, storage.ErrSaveRequestUnknown) {
		outcome = SaveUnknown
	}
	return SaveResult{Object: objectFromMeta(receipt.Meta, nil), Outcome: outcome, Replayed: receipt.Replayed}
}

func (i *Instance) reconcileSaveReceiptLocked(ctx context.Context, result SaveResult, err error) error {
	if result.Outcome == SaveCommitted || result.Outcome == SaveNotCommitted {
		return errors.Join(err, i.refreshPersistentUsageLocked(ctx))
	}
	return err
}

// ResolveSave reports a retained effect without retrying a mutation.
func (i *Instance) ResolveSave(ctx context.Context, bucket, key, requestKey string) (SaveResult, error) {
	unknown := SaveResult{Outcome: SaveUnknown}
	if err := i.checkContext(ctx); err != nil {
		return unknown, err
	}
	if err := i.checkResource(authority.ObjectRead, object(bucket, key)); err != nil {
		return unknown, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return unknown, err
	}
	if err := i.checkContext(ctx); err != nil {
		return unknown, err
	}
	if !i.saveRequestsLocked() {
		return unknown, storage.ErrSaveRequestsUnsupported
	}
	receipt, err := i.store.(storage.SaveRequestStore).ResolveSaveRequest(ctx, bucket, key, requestKey)
	result := saveReceiptResult(receipt, err)
	return result, i.reconcileSaveReceiptLocked(ctx, result, err)
}
