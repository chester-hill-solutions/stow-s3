package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *Instance) CreateMultipartUpload(ctx context.Context, bucket, key string, opts storage.MultipartOptions) (*storage.MultipartUpload, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	if err := i.checkResource(authority.ObjectWrite, policy.Object(bucket, key)); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	if err := i.refreshPersistentUsageLocked(ctx); err != nil {
		return nil, err
	}
	if err := i.admitMultipartUpload(); err != nil {
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
	if !i.finishUpload(upload, exists, err) {
		return nil, err
	}
	clone := storage.CloneMultipartUpload(*upload)
	return &clone, nil
}

func (i *Instance) GetMultipartUpload(ctx context.Context, uploadID string) (*storage.MultipartUpload, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	if err := i.checkUpload(authority.ObjectRead, uploadID); err != nil {
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
	data, err := readBoundedPart(body, i.limits.MaxBytes)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	if err := i.checkUpload(authority.ObjectWrite, uploadID); err != nil {
		return nil, err
	}
	if err := i.refreshPersistentUsageLocked(ctx); err != nil {
		return nil, err
	}
	usage, ok := i.multipart[uploadID]
	if !ok {
		return nil, storage.ErrUploadNotFound
	}
	oldSize := usage.parts[partNumber]
	if !i.bytesQuotaFits(oldSize, int64(len(data))) {
		return nil, ErrQuotaExceeded
	}
	part, err := i.multipartStore.UploadPart(ctx, uploadID, partNumber, bytes.NewReader(data))
	if !i.finishPart(uploadID, part, err) {
		return nil, err
	}
	clone := *part
	return &clone, err
}

func (i *Instance) ListPartsPage(ctx context.Context, uploadID string, opts storage.ListPartsOptions) (*storage.ListPartsResult, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	if err := i.checkUpload(authority.ObjectRead, uploadID); err != nil {
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
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	if err := i.checkUpload(authority.ObjectWrite, uploadID); err != nil {
		return nil, err
	}
	if err := i.refreshPersistentUsageLocked(ctx); err != nil {
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
	if err := i.admitCompletion(uploadID, parts, oldSize, exists); err != nil {
		return nil, err
	}
	meta, err := i.multipartStore.CompleteMultipartUpload(ctx, uploadID, parts)
	if !i.finishCompletion(uploadID, oldSize, meta, exists, err) {
		return nil, err
	}
	if err != nil {
		err = i.reconcileCompletionError(ctx, uploadID, err)
	}
	clone := *meta
	return &clone, err
}

func (i *Instance) reconcileCompletionError(ctx context.Context, uploadID string, cause error) error {
	_, err := i.multipartStore.GetMultipartUpload(ctx, uploadID)
	if errors.Is(err, storage.ErrUploadNotFound) {
		i.removeUpload(uploadID, true)
		return cause
	}
	return errors.Join(cause, err)
}

func (i *Instance) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return err
	}
	if err := i.checkUpload(authority.ObjectWrite, uploadID); err != nil {
		return err
	}
	if err := i.refreshPersistentUsageLocked(ctx); err != nil {
		return err
	}
	usage, ok := i.multipart[uploadID]
	if !ok {
		return storage.ErrUploadNotFound
	}
	_, exists, err := i.objectSize(ctx, usage.upload.Bucket, usage.upload.Key)
	if err != nil {
		return err
	}
	err = i.multipartStore.AbortMultipartUpload(ctx, uploadID)
	i.finishAbort(uploadID, exists, err)
	return err
}

func (i *Instance) ListParts(ctx context.Context, uploadID string) ([]storage.PartInfo, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMultipartOpen(); err != nil {
		return nil, err
	}
	if err := i.checkUpload(authority.ObjectRead, uploadID); err != nil {
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
	if err := i.checkResource(authority.ObjectRead, policy.Object(bucket, key)); err != nil {
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
	if err := i.checkResource(authority.ObjectList, policy.Object(bucket, opts.Prefix)); err != nil {
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

func readBoundedPart(body io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(body)
	}
	data, err := io.ReadAll(io.LimitReader(body, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) == limit {
		var extra [1]byte
		n, err := body.Read(extra[:])
		if err != nil && err != io.EOF {
			return nil, err
		}
		if n > 0 {
			return nil, ErrQuotaExceeded
		}
	}
	return data, nil
}
