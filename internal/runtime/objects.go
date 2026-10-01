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

// The object half of the namespace. Every one of these consults the
// environment's Authority before touching anything, which is what makes the
// answer the same whether a caller arrived over S3 or in process.

func (i *Instance) PutObject(ctx context.Context, bucket, key string, data []byte, options PutOptions) (Object, error) {
	if err := i.checkContext(ctx); err != nil {
		return Object{}, err
	}
	if err := i.checkResource(authority.ObjectWrite, policy.Object(bucket, key)); err != nil {
		return Object{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMutationLocked(ctx); err != nil {
		return Object{}, err
	}
	return i.putObjectLocked(ctx, bucket, key, data, options)
}

func (i *Instance) putObjectLocked(ctx context.Context, bucket, key string, data []byte, options PutOptions) (Object, error) {

	oldSize, exists, err := i.objectSize(ctx, bucket, key)
	if err != nil {
		return Object{}, err
	}
	requestedSize := int64(len(data))
	if !exists && !i.objectQuotaFits(objectTarget(bucket, key), 1) {
		return Object{}, ErrQuotaExceeded
	}
	if !i.bytesQuotaFits(oldSize, requestedSize) {
		return Object{}, ErrQuotaExceeded
	}
	target := objectTarget(bucket, key)
	_, targetReserved := i.reservedTargets[target]
	// Not copied: every store copies the body through ETagForReader. See ByteReader.
	meta, err := i.store.PutObject(ctx, bucket, key, bytes.NewReader(data), storage.PutOptions{
		ContentType:       options.ContentType,
		Metadata:          storage.CloneMetadata(options.Metadata),
		ChecksumAlgorithm: options.ChecksumAlgorithm,
		ChecksumValue:     options.ChecksumValue,
		IfMatch:           options.IfMatch,
		IfNoneMatch:       options.IfNoneMatch,
		Guard:             options.guard,
		RequestKey:        options.requestKey,
	})
	if err != nil && !errors.Is(err, storage.ErrMutationCommitted) {
		return Object{}, err
	}
	if exists {
		i.usage.Bytes -= oldSize
	} else {
		i.usage.Objects++
	}
	i.usage.Bytes += int64(len(data))
	if targetReserved {
		i.consumeTargetReservation(target)
	}
	i.reconcileTargetReservation(target, true)
	return objectFromMeta(meta, nil), err
}

func (i *Instance) GetObject(ctx context.Context, bucket, key string) (Object, error) {
	if err := i.checkContext(ctx); err != nil {
		return Object{}, err
	}
	if err := i.checkResource(authority.ObjectRead, policy.Object(bucket, key)); err != nil {
		return Object{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return Object{}, err
	}
	reader, meta, err := i.store.GetObject(ctx, bucket, key)
	if err != nil {
		return Object{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return Object{}, err
	}
	return objectFromMeta(meta, data), nil
}

func (i *Instance) HeadObject(ctx context.Context, bucket, key string) (Object, error) {
	if err := i.checkContext(ctx); err != nil {
		return Object{}, err
	}
	if err := i.checkResource(authority.ObjectRead, policy.Object(bucket, key)); err != nil {
		return Object{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return Object{}, err
	}
	meta, err := i.store.HeadObject(ctx, bucket, key)
	if err != nil {
		return Object{}, err
	}
	return objectFromMeta(meta, nil), nil
}

// listScope is the set of keys one listing may disclose; a nil scope permits everything, so
// a policy-free deployment is unchanged. The decision is per key: a listing asks for a
// prefix, and `public/secret` is not `public/secret/`.
type listScope struct {
	set    *policy.Set
	env    authority.Authority
	bucket string
}

// newListScope resolves the policy for one listing, failing rather than disclosing.
func (i *Instance) newListScope(bucket string) (listScope, error) {
	if i.policy == nil {
		return listScope{}, nil
	}
	if err := i.authority.Check(authority.ObjectList); err != nil {
		return listScope{}, err
	}
	set, err := i.currentPolicy()
	if err != nil {
		return listScope{}, err
	}
	return listScope{set: set, env: i.authority, bucket: bucket}, nil
}

// allows reports whether one key may be disclosed, on object.list to match S3.
func (s listScope) allows(key string) bool {
	if s.set == nil {
		return true
	}
	return s.set.Allows(s.env, policy.Object(s.bucket, key), authority.ObjectList) == nil
}

func (i *Instance) ListObjects(ctx context.Context, bucket string, options ListOptions) (ObjectPage, error) {
	if err := i.checkContext(ctx); err != nil {
		return ObjectPage{}, err
	}
	if err := i.checkResource(authority.ObjectList, policy.Object(bucket, options.Prefix)); err != nil {
		return ObjectPage{}, err
	}
	if options.Limit < 0 {
		return ObjectPage{}, ErrInvalidListLimit
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return ObjectPage{}, err
	}
	limit := options.Limit
	if limit == 0 {
		limit = 1000
	}
	// Resolved once; checkResource would re-validate per key.

	scope, err := i.newListScope(bucket)
	if err != nil {
		return ObjectPage{}, err
	}

	result, err := i.store.ListObjectsV2(ctx, bucket, storage.ListOptions{
		Prefix:            options.Prefix,
		Delimiter:         options.Delimiter,
		ContinuationToken: options.Cursor,
		MaxKeys:           limit,
		StartAfter:        options.StartAfter,
	})
	if err != nil {
		return ObjectPage{}, err
	}
	objects := make([]Object, 0, len(result.Objects))
	for _, meta := range result.Objects {
		if !scope.allows(meta.Key) {
			continue
		}
		objects = append(objects, objectFromMeta(&meta, nil))
	}
	prefixes := make([]string, 0, len(result.CommonPrefixes))
	for _, prefix := range result.CommonPrefixes {
		if scope.allows(prefix) {
			prefixes = append(prefixes, prefix)
		}
	}
	// The count is of what was returned, not of what the store holds: a count the caller
	// cannot account for is itself a disclosure. Both kinds are counted, which is S3's
	// arithmetic: a delimiter page with no keys and two prefixes reports 2, not 0.
	// Truncation and the cursors stay as the store reported, so a filtered page is short
	// rather than silently complete.
	return ObjectPage{
		Objects:        objects,
		CommonPrefixes: prefixes,
		Truncated:      result.IsTruncated,
		Cursor:         result.ContinuationToken,
		NextCursor:     result.NextContinuationToken,
		KeyCount:       len(objects) + len(prefixes),
	}, nil
}

func (i *Instance) DeleteObjects(ctx context.Context, bucket string, keys []string) ([]string, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, err
	}
	// Each key is authorized on its own: a batch is a convenience, not a single
	// wider permission, and one unauthorized key must not be deleted alongside
	// the rest. Checked before any of them is touched, so a refusal leaves the
	// batch unapplied rather than partly applied.
	for _, key := range keys {
		if err := i.checkResource(authority.ObjectDelete, policy.Object(bucket, key)); err != nil {
			return nil, err
		}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMutationLocked(ctx); err != nil {
		return nil, err
	}
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	for _, key := range keys {
		if err := storage.ValidateKey(key); err != nil {
			return nil, err
		}
	}

	sizes := make(map[string]int64, len(keys))
	for _, key := range keys {
		if err := i.checkContext(ctx); err != nil {
			return nil, err
		}
		meta, err := i.quotaStore().HeadObject(ctx, bucket, key)
		if err == nil {
			sizes[key] = meta.Size
		} else if !errors.Is(err, storage.ErrObjectNotFound) {
			return nil, err
		}
	}
	deleted, err := i.store.DeleteObjects(ctx, bucket, keys)
	for _, key := range deleted {
		size, existed := sizes[key]
		if !existed {
			// Confirmed as deleted because it was not there. There is no object
			// to account for, and decrementing anyway would let a caller inflate
			// its own quota by deleting keys that never existed.
			continue
		}
		delete(sizes, key)
		i.usage.Bytes -= size
		i.usage.Objects--
		target := objectTarget(bucket, key)
		i.consumeTargetReservation(target)
		i.reconcileTargetReservation(target, false)
	}
	return deleted, err
}

func (i *Instance) deleteObjectLocked(ctx context.Context, bucket, key string) error {
	size, exists, err := i.objectSize(ctx, bucket, key)
	if err != nil {
		return err
	}
	err = i.store.DeleteObject(ctx, bucket, key)
	if err != nil && !errors.Is(err, storage.ErrMutationCommitted) {
		return err
	}
	if exists {
		i.usage.Bytes -= size
		i.usage.Objects--
	}
	target := objectTarget(bucket, key)
	i.consumeTargetReservation(target)
	i.reconcileTargetReservation(target, false)
	return err
}

func (i *Instance) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	if err := i.checkResource(authority.ObjectDelete, policy.Object(bucket, key)); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMutationLocked(ctx); err != nil {
		return err
	}
	return i.deleteObjectLocked(ctx, bucket, key)
}

func (i *Instance) CopyObject(ctx context.Context, sourceBucket, sourceKey, destinationBucket, destinationKey string) (Object, error) {
	return i.CopyObjectCond(ctx, storage.CopyRequest{
		SourceBucket: sourceBucket,
		SourceKey:    sourceKey,
		DestBucket:   destinationBucket,
		DestKey:      destinationKey,
	})
}

// CopyObjectCond copies with the source conditions evaluated on the version the store
// copies.
//
// The conditions and the bytes come from one capture inside the store: a caller naming
// an ETag is asking for that version, and a copy that re-read the source after the
// check could publish a different one. When the store cannot evaluate them they are
// checked here against the version this observed, and the copy still happens — a weaker
// guarantee, which storage.ConditionalCopyStore documents.
func (i *Instance) CopyObjectCond(ctx context.Context, req storage.CopyRequest) (Object, error) {
	if err := i.checkContext(ctx); err != nil {
		return Object{}, err
	}
	// Both ends, and the source is not optional: a copy reads the source and
	// discloses its bytes at a destination the caller chose, so checking only the
	// destination lets a write-only caller copy content out. The source goes first, being
	// the read that can disclose.
	if err := i.checkResource(authority.ObjectRead, policy.Object(req.SourceBucket, req.SourceKey)); err != nil {
		return Object{}, err
	}
	if err := i.checkResource(authority.ObjectWrite, policy.Object(req.DestBucket, req.DestKey)); err != nil {
		return Object{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkMutationLocked(ctx); err != nil {
		return Object{}, err
	}
	// The source size for the quota check, and the conditions if the store cannot evaluate
	// them. Both answer about a version the caller may then race.
	sourceMeta, err := i.store.HeadObject(ctx, req.SourceBucket, req.SourceKey)
	if err != nil {
		return Object{}, err
	}
	if err := i.checkCopySourceConditions(req, sourceMeta); err != nil {
		return Object{}, err
	}
	oldSize, exists, err := i.objectSize(ctx, req.DestBucket, req.DestKey)
	if err != nil {
		return Object{}, err
	}
	target := objectTarget(req.DestBucket, req.DestKey)
	if !exists && !i.objectQuotaFits(target, 1) {
		return Object{}, ErrQuotaExceeded
	}
	if !i.bytesQuotaFits(oldSize, sourceMeta.Size) {
		return Object{}, ErrQuotaExceeded
	}
	_, targetReserved := i.reservedTargets[target]
	meta, err := i.storeCopy(ctx, req)
	if err != nil && !errors.Is(err, storage.ErrMutationCommitted) {
		return Object{}, err
	}
	if exists {
		i.usage.Bytes -= oldSize
	} else {
		i.usage.Objects++
	}
	i.usage.Bytes += meta.Size
	if targetReserved {
		i.consumeTargetReservation(target)
	}
	i.reconcileTargetReservation(target, true)
	return objectFromMeta(meta, nil), err
}

// checkCopySourceConditions applies the copy's source conditions when the store
// cannot, and is a no-op when it can. Split out of the copy so the conditions
// are one decision rather than a branch inside a function already carrying the
// resource, quota and accounting steps.
func (i *Instance) checkCopySourceConditions(req storage.CopyRequest, sourceMeta *storage.ObjectMeta) error {
	if i.storeCopyChecksConditions() {
		return nil
	}
	return storage.CheckCopySourceConditions(req.Options, sourceMeta)
}

// storeCopy performs the copy through whichever capability the store offers, so
// a store that can evaluate the conditions on the version it copies is used for
// that rather than being asked to copy blindly.
func (i *Instance) storeCopy(ctx context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	if conditional, ok := i.store.(storage.ConditionalCopyStore); ok {
		return conditional.CopyObjectCond(ctx, req)
	}
	return i.store.CopyObject(ctx, req.SourceBucket, req.SourceKey, req.DestBucket, req.DestKey)
}

// storeCopyChecksConditions reports whether the store evaluates the copy's source
// conditions itself, which decides whether this layer has to.
func (i *Instance) storeCopyChecksConditions() bool {
	_, ok := i.store.(storage.ConditionalCopyStore)
	return ok
}

func objectTarget(bucket, key string) string {
	return bucket + "\x00" + key
}

func (i *Instance) objectSize(ctx context.Context, bucket, key string) (int64, bool, error) {
	meta, err := i.quotaStore().HeadObject(ctx, bucket, key)
	if err == nil {
		return meta.Size, true, nil
	}
	if errors.Is(err, storage.ErrObjectNotFound) {
		return 0, false, nil
	}
	return 0, false, err
}

func objectFromMeta(meta *storage.ObjectMeta, data []byte) Object {
	if meta == nil {
		return Object{Data: append([]byte(nil), data...)}
	}
	return Object{
		Bucket:            meta.Bucket,
		Key:               meta.Key,
		Data:              append([]byte(nil), data...),
		Size:              meta.Size,
		ETag:              meta.ETag,
		VersionID:         meta.VersionID,
		ContentType:       meta.ContentType,
		Metadata:          storage.CloneMetadata(meta.Metadata),
		LastModified:      meta.LastModified,
		ChecksumAlgorithm: meta.ChecksumAlgorithm,
		ChecksumValue:     meta.ChecksumValue,
	}
}

func (i *Instance) quotaStore() storage.Store {
	if provider, ok := i.store.(storage.QuotaStoreProvider); ok {
		return provider.QuotaStore()
	}
	return i.store
}
