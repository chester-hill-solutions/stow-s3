package runtime

import (
	"context"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *Instance) ReplayWorkspaceFileSave(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.SaveReceipt, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := i.checkContext(ctx); err != nil {
		return storage.SaveReceipt{}, false, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.workspaceFileReceiptAccess(ctx, bucket, request.Write.Path, true); err != nil {
		return storage.SaveReceipt{}, false, err
	}
	receipt, found, err := i.store.(storage.WorkspaceFileSaveStore).ReplayWorkspaceFileSave(ctx, bucket, request)
	if found {
		err = i.reconcileWorkspaceFileReceipt(receipt, err)
	} else if workspaceFileReceiptUnknown(receipt, err) {
		i.markWorkspaceFileSaveUnknown()
	}
	return receipt, found, err
}

func (i *Instance) SaveWorkspaceFile(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.SaveReceipt, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := i.checkContext(ctx); err != nil {
		return storage.SaveReceipt{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.workspaceFileReceiptAccess(ctx, bucket, request.Write.Path, true); err != nil {
		return storage.SaveReceipt{}, err
	}
	saver := i.store.(storage.WorkspaceFileSaveStore)
	receipt, found, err := saver.ReplayWorkspaceFileSave(ctx, bucket, request)
	if found || err != nil {
		if found {
			err = i.reconcileWorkspaceFileReceipt(receipt, err)
		} else if workspaceFileReceiptUnknown(receipt, err) {
			i.markWorkspaceFileSaveUnknown()
		}
		return receipt, err
	}
	return i.saveFreshWorkspaceFile(ctx, bucket, request, saver)
}

func (i *Instance) saveFreshWorkspaceFile(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest, saver storage.WorkspaceFileSaveStore) (storage.SaveReceipt, error) {
	_, current, err := i.prepareWorkspaceFilePublication(ctx, bucket, request.Write)
	if err != nil {
		return storage.SaveReceipt{}, err
	}
	if err := i.checkWorkspaceFileQuota(bucket, request.Write.Path, request.Write.Data, current); err != nil {
		return storage.SaveReceipt{}, err
	}
	receipt, err := saver.SaveWorkspaceFile(ctx, bucket, request)
	return receipt, i.finishWorkspaceFileSave(bucket, receipt, err, request, current)
}

func (i *Instance) finishWorkspaceFileSave(bucket string, receipt storage.SaveReceipt, err error, request storage.WorkspaceFileSaveRequest, current storage.WorkspaceFileObservation) error {
	switch {
	case receipt.Replayed && workspaceFileReceiptTerminal(receipt):
		err = errors.Join(err, i.refreshWorkspaceFileReceipt())
	case receipt.Outcome == string(storage.WorkspaceFileCommitted):
		if err != nil && !errors.Is(err, storage.ErrMutationCommitted) {
			err = storage.CommittedError(err)
		}
		i.finishPut(objectTarget(bucket, request.Write.Path), current.Size, int64(len(request.Write.Data)), !current.Absent, err)
	case workspaceFileReceiptUnknown(receipt, err):
		i.markWorkspaceFileSaveUnknown()
	}
	return err
}

func (i *Instance) ResolveWorkspaceFileSave(ctx context.Context, bucket, path, requestKey string) (storage.SaveReceipt, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := i.checkContext(ctx); err != nil {
		return storage.SaveReceipt{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.workspaceFileReceiptAccess(ctx, bucket, path, false); err != nil {
		return storage.SaveReceipt{}, err
	}
	receipt, err := i.store.(storage.WorkspaceFileSaveStore).ResolveWorkspaceFileSave(ctx, bucket, path, requestKey)
	if workspaceFileReceiptTerminal(receipt) {
		err = i.reconcileWorkspaceFileReceipt(receipt, err)
	} else if workspaceFileReceiptUnknown(receipt, err) {
		i.markWorkspaceFileSaveUnknown()
	}
	return receipt, err
}

func (i *Instance) workspaceFileReceiptAccess(ctx context.Context, bucket, path string, write bool) error {
	if err := i.checkOpen(); err != nil {
		return err
	}
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	resource := policy.Object(bucket, path)
	if err := i.checkResource(authority.ObjectRead, resource); err != nil {
		return err
	}
	if write {
		if err := i.checkResource(authority.ObjectWrite, resource); err != nil {
			return err
		}
	}
	if i.policy != nil {
		return storage.ErrWorkspaceFileUnsupported
	}
	reader, ok := i.store.(storage.WorkspaceFileReadStore)
	if !ok || reader.WorkspaceFileBucket() != bucket {
		return storage.ErrWorkspaceFileUnsupported
	}
	if _, ok := i.store.(storage.WorkspaceFileSaveStore); !ok {
		return storage.ErrWorkspaceFileUnsupported
	}
	return nil
}

func workspaceFileReceiptTerminal(receipt storage.SaveReceipt) bool {
	return receipt.Outcome == string(storage.WorkspaceFileCommitted) || receipt.Outcome == string(storage.WorkspaceFileNotCommitted)
}

func workspaceFileReceiptUnknown(receipt storage.SaveReceipt, err error) bool {
	if errors.Is(err, storage.ErrSaveRequestConflict) || errors.Is(err, storage.ErrInvalidSaveRequest) || errors.Is(err, storage.ErrSaveRequestNotFound) || errors.Is(err, storage.ErrSaveRequestNotCommitted) {
		return false
	}
	if workspaceFileReceiptTerminal(receipt) {
		return false
	}
	return receipt.Outcome == string(storage.WorkspaceFileUnknown) || errors.Is(err, storage.ErrSaveRequestUnknown) || errors.Is(err, storage.ErrWorkspaceFileBlocked) || err != nil
}

func (i *Instance) reconcileWorkspaceFileReceipt(receipt storage.SaveReceipt, err error) error {
	if !workspaceFileReceiptTerminal(receipt) {
		if workspaceFileReceiptUnknown(receipt, err) {
			i.markWorkspaceFileSaveUnknown()
		}
		return err
	}
	return errors.Join(err, i.refreshWorkspaceFileReceipt())
}

func (i *Instance) refreshWorkspaceFileReceipt() error {
	if err := i.refreshUsageObservationLocked(context.Background()); err != nil {
		i.usageDirty = true
		i.recoveryErr = err
		return err
	}
	return nil
}

func (i *Instance) markWorkspaceFileSaveUnknown() {
	i.recordMutation(storage.ErrSaveRequestUnknown)
	i.recoveryErr = storage.ErrSaveRequestUnknown
}
