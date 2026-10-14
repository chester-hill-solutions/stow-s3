package embedded

import (
	"context"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type Runtime struct {
	inner                        *stowruntime.Instance
	conditionalWritesUnsupported bool
}

// Open selects memory or an explicitly supplied store.
func Open(options Options) (*Runtime, error) {
	runtimeOptions := stowruntime.Options{
		Backend:             stowruntime.Backend(options.Backend),
		MaxBytes:            options.MaxBytes,
		MaxObjects:          options.MaxObjects,
		MaxMultipartUploads: options.MaxMultipartUploads,
		Authority:           options.Authority,
	}

	var (
		instance *stowruntime.Instance
		err      error
	)
	if options.Store == nil {
		instance, err = stowruntime.Open(runtimeOptions)
	} else {
		// Reset is not offered for a supplied store: it would have to rebuild something the
		// caller owns, and "empty the caller's storage" is not a reasonable thing to do to a
		// directory somebody handed you.
		//
		// Whether the store can serve multipart is not passed here: adaptStore returns a
		// value implementing the internal MultipartStore only when the caller's does.
		instance, err = stowruntime.OpenWithStore(
			runtimeOptions, adaptStore(options.Store), nil,
		)
	}
	if err != nil {
		return nil, mapError(err)
	}
	return &Runtime{
		inner:                        instance,
		conditionalWritesUnsupported: options.Store != nil && !supportsConditionalWrites(options.Store),
	}, nil
}

func (r *Runtime) CreateBucket(ctx context.Context, name string) error {
	return mapError(r.inner.CreateBucket(ctx, name))
}

func (r *Runtime) DeleteBucket(ctx context.Context, name string) error {
	return mapError(r.inner.DeleteBucket(ctx, name))
}

func (r *Runtime) ListBuckets(ctx context.Context) ([]Bucket, error) {
	buckets, err := r.inner.ListBuckets(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]Bucket, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, Bucket{Name: bucket.Name, CreationDate: bucket.CreationDate})
	}
	return out, nil
}

func (r *Runtime) PutObject(ctx context.Context, bucket, key string, data []byte, options PutOptions) (Object, error) {
	object, err := r.inner.PutObject(ctx, bucket, key, data, stowruntime.PutOptions{
		ContentType: options.ContentType,
		Metadata:    storage.CloneMetadata(options.Metadata),
		IfMatch:     options.IfMatch,
		IfNoneMatch: options.IfNoneMatch,
	})
	if err != nil && !errors.Is(err, storage.ErrMutationCommitted) {
		return Object{}, mapError(err)
	}
	return objectOf(object), mapError(err)
}

func (r *Runtime) GetObject(ctx context.Context, bucket, key string) (Object, error) {
	object, err := r.inner.GetObject(ctx, bucket, key)
	if err != nil {
		return Object{}, mapError(err)
	}
	return ownedObjectOf(object), nil
}

func (r *Runtime) HeadObject(ctx context.Context, bucket, key string) (Object, error) {
	object, err := r.inner.HeadObject(ctx, bucket, key)
	if err != nil {
		return Object{}, mapError(err)
	}
	return objectOf(object), nil
}

func (r *Runtime) ListObjects(ctx context.Context, bucket string, options ListOptions) (ObjectPage, error) {
	page, err := r.inner.ListObjects(ctx, bucket, stowruntime.ListOptions{
		Prefix: options.Prefix,
		Cursor: options.Cursor,
		Limit:  options.Limit,
	})
	if err != nil {
		return ObjectPage{}, mapError(err)
	}
	objects := make([]Object, 0, len(page.Objects))
	for _, object := range page.Objects {
		objects = append(objects, objectOf(object))
	}
	return ObjectPage{
		Objects:    objects,
		Truncated:  page.Truncated,
		NextCursor: page.NextCursor,
	}, nil
}

func (r *Runtime) DeleteObject(ctx context.Context, bucket, key string) error {
	return mapError(r.inner.DeleteObject(ctx, bucket, key))
}

func (r *Runtime) CopyObject(ctx context.Context, sourceBucket, sourceKey, destinationBucket, destinationKey string) (Object, error) {
	object, err := r.inner.CopyObject(ctx, sourceBucket, sourceKey, destinationBucket, destinationKey)
	if err != nil {
		return Object{}, mapError(err)
	}
	return objectOf(object), nil
}

// Authorize is the decision for one operation on one resource this Runtime's verbs
// do not cover. Delegating keeps the revision lookup and the effective-access rule in
// one place.
func (r *Runtime) Authorize(op authority.Operation, res policy.Resource) error {
	return r.inner.Authorize(op, res)
}

func (r *Runtime) Reset(ctx context.Context) error {
	return mapError(r.inner.Reset(ctx))
}

func (r *Runtime) Close() error {
	return mapError(r.inner.Close())
}

func (r *Runtime) Usage() Usage {
	usage := r.inner.Usage()
	return Usage{Bytes: usage.Bytes, Objects: usage.Objects}
}

// Authority reports what this environment permits.
func (r *Runtime) Authority() Authority { return r.inner.Authority() }

func (r *Runtime) SupportsConditionalWrites() bool { return !r.conditionalWritesUnsupported }

func (r *Runtime) Capabilities() Capabilities {
	capabilities := r.inner.Capabilities()
	return Capabilities{
		Backend:             Backend(capabilities.Backend),
		MaxBytes:            capabilities.MaxBytes,
		MaxObjects:          capabilities.MaxObjects,
		MaxMultipartUploads: capabilities.MaxMultipartUploads,
		Persistent:          capabilities.Persistent,
		Multipart:           capabilities.Multipart,
		Upstream:            capabilities.Upstream,
		ConditionalWrites:   r.SupportsConditionalWrites(),
		GuardedSaves:        r.SupportsGuardedSaves(),
		DurableSaveRequests: r.SupportsDurableSaveRequests(),
		RecoveryHolds:       r.SupportsRecoveryHolds(),
	}
}

var runtimeErrorMappings = [...][2]error{
	{storage.ErrSaveRequestUnknown, ErrSaveRequestUnknown},
	{stowruntime.ErrClosed, ErrClosed},
	{stowruntime.ErrQuotaExceeded, ErrQuotaExceeded},
	{stowruntime.ErrUnsupportedBackend, ErrUnsupportedBackend},
	{stowruntime.ErrInvalidListLimit, ErrInvalidListLimit},
	{storage.ErrBucketNotFound, ErrBucketNotFound},
	{storage.ErrBucketExists, ErrBucketExists},
	{storage.ErrBucketNotEmpty, ErrBucketNotEmpty},
	{storage.ErrObjectNotFound, ErrObjectNotFound},
	{storage.ErrInvalidBucketName, ErrInvalidBucket},
	{storage.ErrInvalidKey, ErrInvalidKey},
	{storage.ErrPreconditionFailed, ErrPreconditionFailed},
}

func mapError(err error) error {
	if err == nil || errors.Is(err, storage.ErrMutationCommitted) {
		return err
	}
	for _, pair := range runtimeErrorMappings {
		if errors.Is(err, pair[0]) {
			return pair[1]
		}
	}
	return err
}

func objectOf(object stowruntime.Object) Object {
	result := ownedObjectOf(object)
	result.Data = append([]byte(nil), object.Data...)
	return result
}

func ownedObjectOf(object stowruntime.Object) Object {
	return Object{
		Bucket:       object.Bucket,
		Key:          object.Key,
		Data:         object.Data,
		Size:         object.Size,
		ETag:         object.ETag,
		ContentType:  object.ContentType,
		Metadata:     storage.CloneMetadata(object.Metadata),
		LastModified: object.LastModified,
	}
}
