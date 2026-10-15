package runtime

import (
	"context"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *usageState) objectQuotaFits(target string, objectDelta int64) bool {
	reserved := i.reservedObjects
	if _, ok := i.reservedTargets[target]; ok {
		reserved--
	}
	return i.usage.Objects+objectDelta+reserved <= i.limits.MaxObjects
}

func (i *usageState) bytesQuotaFits(oldSize, newSize int64) bool {
	return i.usage.Bytes+i.reservedBytes-oldSize+newSize <= i.limits.MaxBytes
}

func (i *usageState) hasTargetReservation(target string) bool {
	_, ok := i.reservedTargets[target]
	return ok
}

func (i *usageState) consumeTargetReservation(target string) {
	if _, ok := i.reservedTargets[target]; !ok {
		return
	}
	delete(i.reservedTargets, target)
	i.reservedObjects--
}

func (i *usageState) addMultipartTarget(target string) {
	i.multipartTargets[target]++
}

func (i *usageState) removeMultipartTarget(target string) {
	count := i.multipartTargets[target]
	if count <= 1 {
		delete(i.multipartTargets, target)
		return
	}
	i.multipartTargets[target] = count - 1
}

// reserveTarget claims one object-count slot for a multipart target that has
// no committed object yet, and reports whether it did. Callers decide what
// running out of room means: initialization fails the open, while a live
// reconciliation simply leaves the reservation unmade.
func (i *usageState) reserveTarget(target string) bool {
	if i.hasTargetReservation(target) || !i.objectQuotaFits(target, 0) {
		return false
	}
	i.reservedTargets[target] = struct{}{}
	i.reservedObjects++
	return true
}

func (i *usageState) reconcileTargetReservation(target string, objectExists bool) {
	if i.multipartTargets[target] == 0 || objectExists {
		i.consumeTargetReservation(target)
		return
	}
	i.reserveTarget(target)
}

type usageScanner struct {
	usageState
	options        Options
	store          storage.Store
	multipartStore storage.MultipartStore
}

func scanStoredUsage(ctx context.Context, options Options, store storage.Store, quotaStore storage.Store) (usageState, error) {
	multipart, _ := store.(storage.MultipartStore)
	i := usageScanner{usageState: newUsageState(options), options: options, store: quotaStore, multipartStore: multipart}
	buckets, err := quotaStore.ListBuckets(ctx)
	if err != nil {
		return usageState{}, err
	}
	for _, bucket := range buckets {
		if err := i.initializeBucket(ctx, bucket.Name); err != nil {
			return usageState{}, err
		}
	}
	if i.usage.Objects > i.options.MaxObjects-i.reservedObjects || i.usage.Bytes > i.options.MaxBytes-i.reservedBytes || i.reservedObjects > i.options.MaxObjects || i.reservedBytes > i.options.MaxBytes {
		return usageState{}, ErrQuotaExceeded
	}
	return i.usageState, nil
}

func (i *usageScanner) initializeBucket(ctx context.Context, bucket string) error {
	if err := i.initializeObjects(ctx, bucket); err != nil {
		return err
	}
	// Reconciliation is skipped for a store that cannot serve multipart. It used to run
	// unconditionally, so such a store could not be OPENED once it held a bucket: the
	// capability gated the operations and not the initialization. An empty store hid it,
	// because the failing case needs a bucket and a fresh store has none.
	if i.multipartStore == nil {
		return nil
	}
	return i.initializeMultipart(ctx, bucket)
}

func (i *usageScanner) initializeObjects(ctx context.Context, bucket string) error {
	if scanner, ok := i.store.(storage.ObjectUsageStore); ok {
		usage, err := scanner.ScanObjectUsage(ctx, bucket)
		if err != nil {
			return err
		}
		if usage.Bytes < 0 || usage.Objects < 0 || i.usage.Bytes > i.options.MaxBytes-usage.Bytes || i.usage.Objects > i.options.MaxObjects-usage.Objects {
			return ErrQuotaExceeded
		}
		i.usage.Bytes += usage.Bytes
		i.usage.Objects += usage.Objects
		return nil
	}
	continuation := ""
	for {
		result, err := i.store.ListObjectsV2(ctx, bucket, storage.ListOptions{ContinuationToken: continuation, MaxKeys: 1000})
		if err != nil {
			return err
		}
		for _, object := range result.Objects {
			if object.Size < 0 || i.usage.Bytes > i.options.MaxBytes-object.Size {
				return ErrQuotaExceeded
			}
			i.usage.Objects++
			i.usage.Bytes += object.Size
		}
		if !result.IsTruncated {
			return nil
		}
		if result.NextContinuationToken == "" || result.NextContinuationToken == continuation {
			return fmt.Errorf("runtime store returned an invalid continuation token for bucket %q", bucket)
		}
		continuation = result.NextContinuationToken
	}
}

func (i *usageScanner) initializeMultipart(ctx context.Context, bucket string) error {
	markers := storage.MultipartListOptions{MaxUploads: 1000}
	for {
		result, err := i.multipartStore.ListMultipartUploads(ctx, bucket, markers)
		if err != nil {
			return err
		}
		for _, upload := range result.Uploads {
			if err := i.initializeMultipartUpload(ctx, upload); err != nil {
				return err
			}
		}
		if !result.IsTruncated {
			return nil
		}
		if result.NextKeyMarker == markers.KeyMarker && result.NextUploadIDMarker == markers.UploadIDMarker {
			return fmt.Errorf("runtime store returned an invalid multipart marker for bucket %q", bucket)
		}
		markers.KeyMarker = result.NextKeyMarker
		markers.UploadIDMarker = result.NextUploadIDMarker
	}
}

func (i *usageScanner) initializeMultipartUpload(ctx context.Context, upload storage.MultipartUpload) error {
	if err := i.admitMultipartUpload(); err != nil {
		return err
	}
	parts, err := i.multipartStore.ListParts(ctx, upload.UploadID)
	if err != nil {
		return err
	}
	partSizes := make(map[int]int64, len(parts))
	var uploadBytes int64
	for _, part := range parts {
		if part.Size < 0 || uploadBytes > i.options.MaxBytes-part.Size {
			return ErrQuotaExceeded
		}
		partSizes[part.PartNumber] = part.Size
		uploadBytes += part.Size
	}
	if i.reservedBytes > i.options.MaxBytes-uploadBytes {
		return ErrQuotaExceeded
	}
	target := objectTarget(upload.Bucket, upload.Key)
	i.multipart[upload.UploadID] = multipartUsage{upload: upload, parts: partSizes}
	i.reservedBytes += uploadBytes
	i.addMultipartTarget(target)
	if i.multipartTargets[target] != 1 {
		return nil
	}
	_, exists, err := storedObjectSize(ctx, i.store, upload.Bucket, upload.Key)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if !i.reserveTarget(target) {
		return ErrQuotaExceeded
	}
	return nil
}

func (i *usageState) admitMultipartUpload() error {
	if int64(len(i.multipart)) >= i.limits.MaxMultipartUploads {
		return fmt.Errorf("%w: maximum outstanding multipart uploads (%d) reached; abort uploads or raise Go MaxMultipartUploads to reopen existing data", ErrQuotaExceeded, i.limits.MaxMultipartUploads)
	}
	return nil
}
