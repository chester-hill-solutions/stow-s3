package adapterbridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"sync"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence"
)

type Store struct {
	mu      sync.Mutex
	backend persistence.Adapter
	buckets map[string]bool
}

var _ storage.Store = (*Store)(nil)
var _ storage.ConditionalCopyStore = (*Store)(nil)

func Bind(backend persistence.Adapter, buckets ...string) (*Store, error) {
	if backend == nil {
		return nil, errors.New("adapterbridge: persistence adapter is required")
	}
	store := &Store{backend: backend, buckets: map[string]bool{}}
	for _, name := range buckets {
		if err := store.CreateBucket(context.Background(), name); err != nil && !errors.Is(err, storage.ErrBucketExists) {
			return nil, err
		}
	}
	return store, nil
}

func (s *Store) CreateBucket(ctx context.Context, name string) error {
	if err := storage.ValidateBucketName(name); err != nil {
		return err
	}
	if admin, ok := s.backend.(persistence.BucketAdmin); ok {
		if err := admin.CreateBucket(ctx, name); err != nil && !errors.Is(err, persistence.ErrBucketExists) {
			return translate(err)
		}
		return nil
	}
	s.mu.Lock()
	s.buckets[name] = true
	s.mu.Unlock()
	return nil
}

func (s *Store) DeleteBucket(ctx context.Context, name string) error {
	exists, err := s.exists(ctx, name)
	if err != nil {
		return translate(err)
	}
	if !exists {
		return storage.ErrBucketNotFound
	}
	page, err := s.backend.List(ctx, name, persistence.ListOptions{MaxKeys: 1})
	if err != nil {
		return translate(err)
	}
	if page != nil && len(page.Objects) > 0 {
		return storage.ErrBucketNotEmpty
	}
	if admin, ok := s.backend.(persistence.BucketAdmin); ok {
		if err := admin.DeleteBucket(ctx, name); err != nil && !errors.Is(err, persistence.ErrBucketNotFound) {
			return translate(err)
		}
		return nil
	}
	s.mu.Lock()
	delete(s.buckets, name)
	s.mu.Unlock()
	return nil
}

func (s *Store) HeadBucket(ctx context.Context, name string) (*storage.BucketInfo, error) {
	exists, err := s.exists(ctx, name)
	if err != nil {
		return nil, translate(err)
	}
	if !exists {
		return nil, storage.ErrBucketNotFound
	}
	return &storage.BucketInfo{Name: name}, nil
}

func (s *Store) ListBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	var names []string
	if admin, ok := s.backend.(persistence.BucketAdmin); ok {
		listed, err := admin.ListBuckets(ctx)
		if err != nil {
			return nil, translate(err)
		}
		names = listed
	} else {
		s.mu.Lock()
		for name := range s.buckets {
			names = append(names, name)
		}
		s.mu.Unlock()
	}
	sort.Strings(names)
	out := make([]storage.BucketInfo, 0, len(names))
	for _, name := range names {
		out = append(out, storage.BucketInfo{Name: name})
	}
	return out, nil
}

func (s *Store) PutObject(ctx context.Context, bucket, key string, body io.Reader, options storage.PutOptions) (*storage.ObjectMeta, error) {
	if body == nil {
		body = bytes.NewReader(nil)
	}
	if options.IfMatch != "" && options.IfNoneMatch != "" {
		return nil, storage.ErrPreconditionFailed
	}
	if options.IfMatch != "" || options.IfNoneMatch != "" {
		meta, err := s.conditionalPut(ctx, bucket, key, body, options)
		if err != nil {
			return nil, translate(err)
		}
		return s.mapMeta(bucket, key, meta)
	}
	meta, err := s.put(ctx, bucket, key, body, options)
	if err != nil {
		return nil, translate(err)
	}
	return s.mapMeta(bucket, key, meta)
}

func (s *Store) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	object, err := s.backend.Get(ctx, bucket, key)
	if err != nil {
		return nil, nil, translate(err)
	}
	meta, err := s.mapMeta(bucket, key, &object.Meta)
	if err != nil {
		object.Body.Close()
		return nil, nil, err
	}
	return object.Body, meta, nil
}

func (s *Store) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	if header, ok := s.backend.(persistence.Header); ok {
		meta, err := header.Head(ctx, bucket, key)
		if err != nil {
			return nil, translate(err)
		}
		return s.mapMeta(bucket, key, meta)
	}
	object, err := s.backend.Get(ctx, bucket, key)
	if err != nil {
		return nil, translate(err)
	}
	object.Body.Close()
	return s.mapMeta(bucket, key, &object.Meta)
}

func (s *Store) DeleteObject(ctx context.Context, bucket, key string) error {
	err := s.backend.Delete(ctx, bucket, key)
	if errors.Is(err, persistence.ErrNotFound) {
		return nil
	}
	return translate(err)
}

func (s *Store) DeleteObjects(ctx context.Context, bucket string, keys []string) ([]string, error) {
	confirmed := make([]string, 0, len(keys))
	for _, key := range keys {
		if err := s.DeleteObject(ctx, bucket, key); err != nil {
			return confirmed, err
		}
		confirmed = append(confirmed, key)
	}
	return confirmed, nil
}

func (s *Store) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (*storage.ObjectMeta, error) {
	return s.CopyObjectCond(ctx, storage.CopyRequest{
		SourceBucket: srcBucket, SourceKey: srcKey, DestBucket: dstBucket, DestKey: dstKey,
	})
}

func (s *Store) CopyObjectCond(ctx context.Context, request storage.CopyRequest) (*storage.ObjectMeta, error) {
	source, err := s.backend.Get(ctx, request.SourceBucket, request.SourceKey)
	if err != nil {
		return nil, translate(err)
	}
	defer source.Body.Close()
	if err := checkSource(source.Meta, request.Options); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(source.Body)
	if err != nil {
		return nil, err
	}
	meta, err := s.put(ctx, request.DestBucket, request.DestKey, bytes.NewReader(body), storage.PutOptions{
		ContentType: source.Meta.ContentType, Metadata: source.Meta.Metadata,
	})
	if err != nil {
		return nil, translate(err)
	}
	return s.mapMeta(request.DestBucket, request.DestKey, meta)
}

func (s *Store) ListObjectsV2(ctx context.Context, bucket string, options storage.ListOptions) (*storage.ListResult, error) {
	page, err := s.backend.List(ctx, bucket, persistence.ListOptions{
		Prefix: options.Prefix, Delimiter: options.Delimiter,
		ContinuationToken: options.ContinuationToken, StartAfter: options.StartAfter, MaxKeys: options.MaxKeys,
	})
	if err != nil {
		return nil, translate(err)
	}
	if page == nil {
		return &storage.ListResult{}, nil
	}
	objects := make([]storage.ObjectMeta, 0, len(page.Objects))
	for _, meta := range page.Objects {
		mapped, err := s.mapMeta(bucket, meta.Key, &meta)
		if err != nil {
			return nil, err
		}
		objects = append(objects, *mapped)
	}
	return &storage.ListResult{
		Objects: objects, CommonPrefixes: append([]string(nil), page.CommonPrefixes...),
		IsTruncated: page.IsTruncated, ContinuationToken: options.ContinuationToken,
		NextContinuationToken: page.NextContinuationToken, KeyCount: len(objects) + len(page.CommonPrefixes),
	}, nil
}

func (s *Store) Close() error {
	if closer, ok := s.backend.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (s *Store) exists(ctx context.Context, bucket string) (bool, error) {
	if admin, ok := s.backend.(persistence.BucketAdmin); ok {
		names, err := admin.ListBuckets(ctx)
		if err != nil {
			return false, err
		}
		return contains(names, bucket), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buckets[bucket], nil
}

func (s *Store) put(ctx context.Context, bucket, key string, body io.Reader, options storage.PutOptions) (*persistence.Meta, error) {
	return s.backend.Put(ctx, bucket, key, body, persistence.PutOptions{
		ContentType: options.ContentType, Metadata: options.Metadata,
		ChecksumAlgorithm: options.ChecksumAlgorithm, ChecksumValue: options.ChecksumValue,
	})
}

func (s *Store) conditionalPut(ctx context.Context, bucket, key string, body io.Reader, options storage.PutOptions) (*persistence.Meta, error) {
	return s.backend.ConditionalPut(ctx, bucket, key, body, persistence.ConditionalPutOptions{
		PutOptions: persistence.PutOptions{
			ContentType: options.ContentType, Metadata: options.Metadata,
			ChecksumAlgorithm: options.ChecksumAlgorithm, ChecksumValue: options.ChecksumValue,
		},
		Condition: persistence.Condition{IfMatch: options.IfMatch, IfAbsent: options.IfNoneMatch != ""},
	})
}

func (s *Store) mapMeta(bucket, key string, meta *persistence.Meta) (*storage.ObjectMeta, error) {
	if meta == nil || meta.ETag == "" {
		return nil, persistence.ErrInvalidResult
	}
	out := &storage.ObjectMeta{
		Bucket: bucket, Key: key, Size: meta.Size, ETag: meta.ETag, ContentType: meta.ContentType,
		LastModified: meta.Modified, Metadata: storage.CloneMetadata(meta.Metadata),
		ChecksumAlgorithm: meta.ChecksumAlgorithm, ChecksumValue: meta.ChecksumValue,
	}
	if out.Key == "" {
		out.Key = key
	}
	return out, nil
}

func checkSource(meta persistence.Meta, options storage.CopyOptions) error {
	if options.SourceIfMatch != "" && options.SourceIfMatch != "*" && options.SourceIfMatch != meta.ETag {
		return storage.ErrPreconditionFailed
	}
	if options.SourceIfNoneMatch == "*" || options.SourceIfNoneMatch != "" && options.SourceIfNoneMatch == meta.ETag {
		return storage.ErrPreconditionFailed
	}
	return nil
}

func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, persistence.ErrNotFound):
		return storage.ErrObjectNotFound
	case errors.Is(err, persistence.ErrBucketNotFound):
		return storage.ErrBucketNotFound
	case errors.Is(err, persistence.ErrBucketExists):
		return storage.ErrBucketExists
	case errors.Is(err, persistence.ErrBucketNotEmpty):
		return storage.ErrBucketNotEmpty
	case errors.Is(err, persistence.ErrPreconditionFailed):
		return storage.ErrPreconditionFailed
	case errors.Is(err, persistence.ErrInvalidKey):
		return storage.ErrInvalidKey
	default:
		return err
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
