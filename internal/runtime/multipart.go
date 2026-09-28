package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *Instance) CreateMultipartUpload(ctx context.Context, bucket, key string, opts storage.MultipartOptions) (*storage.MultipartUpload, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := i.check(authority.ObjectWrite); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	target := objectTarget(bucket, key)
	_, exists, err := i.objectSize(ctx, bucket, key)
	if err != nil {
		return nil, err
	}
	// A target that already holds a reservation has spent its object slot, so
	// only a first upload for a missing object needs room for one.
	needsObjectSlot := !exists && !i.hasTargetReservation(target)
	if needsObjectSlot && !i.objectQuotaFits(target, 1) {
		return nil, ErrQuotaExceeded
	}
	upload, err := i.multipartStore.CreateMultipartUpload(ctx, bucket, key, opts)
	if err != nil {
		return nil, err
	}
	clone := storage.CloneMultipartUpload(*upload)
	i.multipart[upload.UploadID] = multipartUsage{upload: clone, parts: make(map[int]int64)}
	i.addMultipartTarget(target)
	if needsObjectSlot {
		// The check above passed under this lock, so the reservation is
		// guaranteed; declining it here would leave the upload uncounted.
		i.reserveTarget(target)
	}
	return &clone, nil
}

func (i *Instance) GetMultipartUpload(ctx context.Context, uploadID string) (*storage.MultipartUpload, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := i.check(authority.ObjectRead); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	upload, err := i.multipartStore.GetMultipartUpload(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	clone := storage.CloneMultipartUpload(*upload)
	return &clone, nil
}

func (i *Instance) UploadPart(ctx context.Context, uploadID string, partNumber int, body io.Reader) (*storage.PartInfo, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := i.check(authority.ObjectWrite); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	usage, ok := i.multipart[uploadID]
	if !ok {
		return nil, storage.ErrUploadNotFound
	}
	oldSize := usage.parts[partNumber]
	if !i.bytesWithinQuota(oldSize, int64(len(data))) {
		return nil, ErrQuotaExceeded
	}
	part, err := i.multipartStore.UploadPart(ctx, uploadID, partNumber, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	usage.parts[partNumber] = part.Size
	i.multipart[uploadID] = usage
	i.reservedBytes += part.Size - oldSize
	clone := *part
	return &clone, nil
}

func (i *Instance) ListPartsPage(ctx context.Context, uploadID string, opts storage.ListPartsOptions) (*storage.ListPartsResult, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := i.check(authority.ObjectRead); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	if lister, ok := i.store.(interface {
		ListPartsPage(context.Context, string, storage.ListPartsOptions) (*storage.ListPartsResult, error)
	}); ok {
		result, err := lister.ListPartsPage(ctx, uploadID, opts)
		if err != nil {
			return nil, err
		}
		clone := *result
		clone.Parts = append([]storage.PartInfo(nil), result.Parts...)
		return &clone, nil
	}
	parts, err := i.multipartStore.ListParts(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	return storage.PaginateParts(parts, opts), nil
}

func (i *Instance) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := i.check(authority.ObjectWrite); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	uploadUsage, ok := i.multipart[uploadID]
	if !ok {
		return nil, storage.ErrUploadNotFound
	}
	oldSize, exists, err := i.objectSize(ctx, uploadUsage.upload.Bucket, uploadUsage.upload.Key)
	if err != nil {
		return nil, err
	}
	// completedBytes is what the object will weigh: the selected parts, in the
	// order they will be concatenated.
	var completedBytes int64
	for _, part := range parts {
		completedBytes += uploadUsage.parts[part.PartNumber]
	}
	target := objectTarget(uploadUsage.upload.Bucket, uploadUsage.upload.Key)
	if !exists && !i.objectQuotaFits(target, 1) {
		return nil, ErrQuotaExceeded
	}
	// The parts of an upload in flight are already reserved, so the only
	// accounting question left is whether the completion itself fits.
	uploadReservation := uploadUsage.bytes()
	if uploadReservation > i.reservedBytes {
		return nil, ErrQuotaExceeded
	}
	// A completion holds the parts and assembles a second copy of the same bytes
	// before the object is published, so for the length of this call the bounded
	// state is every committed object, every in-flight part, and the buffer
	// being assembled. That last term is the completion reservation, and it is
	// what used to be missing: the store transiently held the object twice while
	// the advertised ceiling counted it once, so a memory-backed session could
	// exceed the bound it publishes by one object per in-flight completion.
	//
	// The check precedes the store call, so a completion that cannot fit is
	// refused before anything is assembled and reserves nothing. A completion
	// that fails afterwards leaves the upload's own reservation in place, which
	// is correct: the upload is still in flight and still holding its parts.
	peak := i.usage.Bytes - oldSize + i.reservedBytes + completedBytes
	if peak > i.options.MaxBytes {
		return nil, ErrQuotaExceeded
	}
	meta, err := i.multipartStore.CompleteMultipartUpload(ctx, uploadID, parts)
	if err != nil && !errors.Is(err, storage.ErrMutationCommitted) {
		return nil, err
	}
	// One transition, from in-flight to committed: the parts' reservation is
	// released and the object's bytes are counted, so the same bytes are never
	// counted twice and never dropped. This is the only place the two states
	// meet, which is what makes "exactly once" a property rather than a hope.
	if exists {
		i.usage.Bytes -= oldSize
	} else {
		i.usage.Objects++
	}
	i.usage.Bytes += meta.Size
	i.reservedBytes -= uploadReservation
	i.removeMultipartTarget(target)
	i.consumeTargetReservation(target)
	i.reconcileTargetReservation(target, true)
	delete(i.multipart, uploadID)
	clone := *meta
	return &clone, err
}

func (i *Instance) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	if err := i.check(authority.ObjectWrite); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return err
	}
	usage, ok := i.multipart[uploadID]
	if !ok {
		return storage.ErrUploadNotFound
	}
	if err := i.multipartStore.AbortMultipartUpload(ctx, uploadID); err != nil {
		return err
	}
	i.reservedBytes -= usage.bytes()
	target := objectTarget(usage.upload.Bucket, usage.upload.Key)
	i.removeMultipartTarget(target)
	i.consumeTargetReservation(target)
	i.reconcileTargetReservation(target, false)
	delete(i.multipart, uploadID)
	return nil
}

func (i *Instance) ListParts(ctx context.Context, uploadID string) ([]storage.PartInfo, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := i.check(authority.ObjectRead); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	parts, err := i.multipartStore.ListParts(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	return append([]storage.PartInfo(nil), parts...), nil
}

func (i *Instance) ValidateMultipartUpload(ctx context.Context, uploadID, bucket, key string) error {
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	if err := i.check(authority.ObjectRead); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return err
	}
	return i.multipartStore.ValidateMultipartUpload(ctx, uploadID, bucket, key)
}

func (i *Instance) ListMultipartUploads(ctx context.Context, bucket string, opts storage.MultipartListOptions) (*storage.MultipartListResult, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := i.check(authority.ObjectList); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	result, err := i.multipartStore.ListMultipartUploads(ctx, bucket, opts)
	if err != nil {
		return nil, err
	}
	clone := *result
	clone.Uploads = append([]storage.MultipartUpload(nil), result.Uploads...)
	return &clone, nil
}

func (u multipartUsage) bytes() int64 {
	var total int64
	for _, size := range u.parts {
		total += size
	}
	return total
}

func (i *Instance) checkMultipartOpen() error {
	if i.multipartStore == nil {
		return ErrMultipartUnsupported
	}
	return i.checkOpen()
}

func (i *Instance) bytesWithinQuota(oldSize, newSize int64) bool {
	return i.usage.Bytes+i.reservedBytes-oldSize+newSize <= i.options.MaxBytes
}
