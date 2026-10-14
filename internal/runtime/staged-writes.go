package runtime

import (
	"context"
	"fmt"
	"io"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type stagedWriteRequest struct {
	bucket  string
	key     string
	body    io.Reader
	size    int64
	options PutOptions
	epoch   uint64
}

type streamReservation struct{ bytes, objects int64 }

func (i *Instance) putStagedObject(ctx context.Context, staged storage.StagedWriteStore, write stagedWriteRequest) (Object, error) {
	i.mu.Lock()
	reservation, err := i.reserveStreamLocked(ctx, write)
	i.mu.Unlock()
	if err != nil {
		return Object{}, err
	}
	defer func() {
		i.mu.Lock()
		if i.storeEpoch == write.epoch {
			i.releaseStaged(reservation)
		}
		i.mu.Unlock()
	}()
	pending, err := staged.StageObject(ctx, write.bucket, write.key, write.body, streamStorageOptions(write.options))
	if err != nil {
		return Object{}, err
	}
	if pending == nil {
		return Object{}, fmt.Errorf("staged store returned no pending object")
	}
	defer pending.Abort(context.Background())
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.storeEpoch != write.epoch {
		return Object{}, fmt.Errorf("runtime store changed while object was staged")
	}
	i.releaseStaged(reservation)
	reservation = streamReservation{}
	if err := i.checkMutationLocked(ctx); err != nil {
		return Object{}, err
	}
	if err := i.checkResource(authority.ObjectWrite, policy.Object(write.bucket, write.key)); err != nil {
		return Object{}, err
	}
	oldSize, exists, err := i.checkStreamQuotaLocked(ctx, write)
	if err != nil {
		return Object{}, err
	}
	meta, err := pending.Commit(ctx)
	if !i.finishPut(objectTarget(write.bucket, write.key), oldSize, write.size, exists, err) {
		return Object{}, err
	}
	return objectFromMeta(meta, nil), err
}

func streamStorageOptions(options PutOptions) storage.PutOptions {
	return storage.PutOptions{ComputeChecksum: options.computeChecksum, ContentType: options.ContentType, Metadata: storage.CloneMetadata(options.Metadata), ChecksumAlgorithm: options.ChecksumAlgorithm, ChecksumValue: options.ChecksumValue, IfMatch: options.IfMatch, IfNoneMatch: options.IfNoneMatch, Guard: options.guard, RequestKey: options.requestKey}
}

func (i *Instance) checkStreamQuotaLocked(ctx context.Context, write stagedWriteRequest) (int64, bool, error) {
	oldSize, exists, err := i.objectSize(ctx, write.bucket, write.key)
	if err != nil {
		return 0, false, err
	}
	if !exists && !i.objectQuotaFits(objectTarget(write.bucket, write.key), 1) {
		return 0, false, ErrQuotaExceeded
	}
	if !i.bytesQuotaFits(oldSize, write.size) {
		return 0, false, ErrQuotaExceeded
	}
	return oldSize, exists, nil
}

func (i *Instance) reserveStreamLocked(ctx context.Context, write stagedWriteRequest) (streamReservation, error) {
	if i.storeEpoch != write.epoch {
		return streamReservation{}, fmt.Errorf("runtime store changed before object staging")
	}
	if err := i.checkMutationLocked(ctx); err != nil {
		return streamReservation{}, err
	}
	oldSize, exists, err := i.checkStreamQuotaLocked(ctx, write)
	if err != nil {
		return streamReservation{}, err
	}
	reservation := streamReservation{bytes: write.size - oldSize}
	if reservation.bytes < 0 {
		reservation.bytes = 0
	}
	if !exists && !i.hasTargetReservation(objectTarget(write.bucket, write.key)) {
		reservation.objects = 1
	}
	i.reserveStaged(reservation)
	return reservation, nil
}
