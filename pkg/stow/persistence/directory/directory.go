package directory

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence"
)

type Adapter struct {
	store *fs.FilesystemStore
}

func Open(root string) (*Adapter, error) {
	store, err := fs.NewFilesystemStore(root)
	if err != nil {
		return nil, err
	}
	return &Adapter{store: store}, nil
}

func (a *Adapter) Get(ctx context.Context, bucket, key string) (*persistence.Object, error) {
	body, meta, err := a.store.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, translate(err)
	}
	return &persistence.Object{Meta: metaOf(meta), Body: body}, nil
}

func (a *Adapter) Head(ctx context.Context, bucket, key string) (*persistence.Meta, error) {
	meta, err := a.store.HeadObject(ctx, bucket, key)
	if err != nil {
		return nil, translate(err)
	}
	mapped := metaOf(meta)
	return &mapped, nil
}

func (a *Adapter) Put(ctx context.Context, bucket, key string, body io.Reader, options persistence.PutOptions) (*persistence.Meta, error) {
	meta, err := a.store.PutObject(ctx, bucket, key, body, storage.PutOptions{
		ContentType: options.ContentType, Metadata: options.Metadata,
		ChecksumAlgorithm: options.ChecksumAlgorithm, ChecksumValue: options.ChecksumValue,
	})
	if err != nil {
		return nil, translate(err)
	}
	mapped := metaOf(meta)
	return &mapped, nil
}

func (a *Adapter) ConditionalPut(ctx context.Context, bucket, key string, body io.Reader, conditional persistence.ConditionalPutOptions) (*persistence.Meta, error) {
	options := storage.PutOptions{
		ContentType: conditional.ContentType, Metadata: conditional.Metadata,
		ChecksumAlgorithm: conditional.ChecksumAlgorithm, ChecksumValue: conditional.ChecksumValue,
	}
	if conditional.Condition.IfAbsent {
		options.IfNoneMatch = "*"
	} else {
		options.IfMatch = conditional.Condition.IfMatch
	}
	meta, err := a.store.PutObject(ctx, bucket, key, body, options)
	if err != nil {
		return nil, translate(err)
	}
	mapped := metaOf(meta)
	return &mapped, nil
}

func (a *Adapter) List(ctx context.Context, bucket string, options persistence.ListOptions) (*persistence.ListResult, error) {
	result, err := a.store.ListObjectsV2(ctx, bucket, storage.ListOptions{
		Prefix: options.Prefix, Delimiter: options.Delimiter,
		ContinuationToken: options.ContinuationToken, StartAfter: options.StartAfter, MaxKeys: options.MaxKeys,
	})
	if err != nil {
		return nil, translate(err)
	}
	objects := make([]persistence.Meta, 0, len(result.Objects))
	for _, meta := range result.Objects {
		objects = append(objects, metaOf(&meta))
	}
	return &persistence.ListResult{
		Objects: objects, CommonPrefixes: append([]string(nil), result.CommonPrefixes...),
		IsTruncated: result.IsTruncated, NextContinuationToken: result.NextContinuationToken,
	}, nil
}

func (a *Adapter) Delete(ctx context.Context, bucket, key string) error {
	return translate(a.store.DeleteObject(ctx, bucket, key))
}

func (a *Adapter) CreateBucket(ctx context.Context, name string) error {
	return translate(a.store.CreateBucket(ctx, name))
}

func (a *Adapter) DeleteBucket(ctx context.Context, name string) error {
	return translate(a.store.DeleteBucket(ctx, name))
}

func (a *Adapter) ListBuckets(ctx context.Context) ([]string, error) {
	buckets, err := a.store.ListBuckets(ctx)
	if err != nil {
		return nil, translate(err)
	}
	names := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		names = append(names, bucket.Name)
	}
	return names, nil
}

func (a *Adapter) Close() error {
	return a.store.Close()
}

func metaOf(meta *storage.ObjectMeta) persistence.Meta {
	if meta == nil {
		return persistence.Meta{}
	}
	return persistence.Meta{
		Key: meta.Key, Size: meta.Size, ETag: meta.ETag, Modified: meta.LastModified,
		ContentType: meta.ContentType, Metadata: storage.CloneMetadata(meta.Metadata),
		ChecksumAlgorithm: meta.ChecksumAlgorithm, ChecksumValue: meta.ChecksumValue,
	}
}

func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrObjectNotFound):
		return fmt.Errorf("%w: %v", persistence.ErrNotFound, err)
	case errors.Is(err, storage.ErrBucketNotFound):
		return fmt.Errorf("%w: %v", persistence.ErrBucketNotFound, err)
	case errors.Is(err, storage.ErrBucketExists):
		return fmt.Errorf("%w: %v", persistence.ErrBucketExists, err)
	case errors.Is(err, storage.ErrBucketNotEmpty):
		return fmt.Errorf("%w: %v", persistence.ErrBucketNotEmpty, err)
	case errors.Is(err, storage.ErrPreconditionFailed):
		return fmt.Errorf("%w: %v", persistence.ErrPreconditionFailed, err)
	case errors.Is(err, storage.ErrInvalidKey):
		return fmt.Errorf("%w: %v", persistence.ErrInvalidKey, err)
	default:
		return err
	}
}
