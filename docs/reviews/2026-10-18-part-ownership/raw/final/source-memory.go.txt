package storage

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

type memObject struct {
	data []byte
	meta ObjectMeta
}

// cloneObjectMeta keeps returned metadata independent from the store's state.
func cloneObjectMeta(meta ObjectMeta) ObjectMeta {
	meta.Metadata = CloneMetadata(meta.Metadata)
	return meta
}

type memPart struct {
	info PartInfo
	data []byte
}

type memMultipart struct {
	upload MultipartUpload
	parts  map[int]memPart
}

type memBucket struct {
	info      BucketInfo
	objects   map[string]*memObject
	multipart map[string]*memMultipart
}

// MemoryStore is an in-memory Store implementation for tests.
type MemoryStore struct {
	mu      sync.RWMutex
	buckets map[string]*memBucket
}

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{buckets: make(map[string]*memBucket)}
}

func (s *MemoryStore) bucket(name string) (*memBucket, error) {
	b, ok := s.buckets[name]
	if !ok {
		return nil, ErrBucketNotFound
	}
	return b, nil
}

func (s *MemoryStore) CreateBucket(_ context.Context, name string) error {
	if err := ValidateBucketName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.buckets[name]; ok {
		return ErrBucketExists
	}
	now := time.Now().UTC()
	s.buckets[name] = &memBucket{
		info:      BucketInfo{Name: name, CreationDate: now},
		objects:   make(map[string]*memObject),
		multipart: make(map[string]*memMultipart),
	}
	return nil
}

func (s *MemoryStore) DeleteBucket(_ context.Context, name string) error {
	if err := ValidateBucketName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[name]
	if !ok {
		return ErrBucketNotFound
	}
	if len(b.objects) > 0 || len(b.multipart) > 0 {
		return ErrBucketNotEmpty
	}
	delete(s.buckets, name)
	return nil
}

func (s *MemoryStore) HeadBucket(_ context.Context, name string) (*BucketInfo, error) {
	if err := ValidateBucketName(name); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.buckets[name]
	if !ok {
		return nil, ErrBucketNotFound
	}
	info := b.info
	return &info, nil
}

func (s *MemoryStore) ListBuckets(_ context.Context) ([]BucketInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]BucketInfo, 0, len(s.buckets))
	for _, b := range s.buckets {
		out = append(out, b.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *MemoryStore) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts PutOptions) (*ObjectMeta, error) {
	pending, err := s.StageObject(ctx, bucket, key, body, opts)
	if err != nil {
		return nil, err
	}
	defer pending.Abort(context.Background())
	return pending.Commit(ctx)
}

func (s *MemoryStore) StageObject(ctx context.Context, bucket, key string, body io.Reader, opts PutOptions) (StagedObject, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if err := ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	// Reject missing buckets before preparation, then recheck at publication.
	s.mu.RLock()
	_, bucketExists := s.buckets[bucket]
	s.mu.RUnlock()
	if !bucketExists {
		return nil, ErrBucketNotFound
	}

	if opts.Guard != nil {
		guard := *opts.Guard
		opts.Guard = &guard
	}
	prepared, err := PreparePut(body, opts)
	if err != nil {
		return nil, err
	}
	return &memoryStagedObject{store: s, bucket: bucket, key: key, prepared: &prepared}, nil
}

func (s *MemoryStore) commitMemoryPut(pending *memoryStagedObject) (*ObjectMeta, error) {
	bucket, key := pending.bucket, pending.key
	prepared := pending.prepared
	opts := prepared.Options

	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return nil, ErrBucketNotFound
	}
	var existing *ObjectMeta
	if object, exists := b.objects[key]; exists {
		existingMeta := cloneObjectMeta(object.meta)
		existing = &existingMeta
	}
	if err := CheckWritePreconditions(opts, existing); err != nil {
		return nil, err
	}
	if err := checkMemoryGuard(opts.Guard, b.objects[key]); err != nil {
		return nil, err
	}
	versionID, err := NewRecordVersion()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	meta := ObjectMeta{
		Bucket:            bucket,
		Key:               key,
		VersionID:         versionID,
		Size:              int64(len(prepared.Data)),
		ETag:              prepared.ETag,
		ContentType:       opts.ContentType,
		LastModified:      now,
		Metadata:          opts.Metadata,
		ChecksumAlgorithm: opts.ChecksumAlgorithm,
		ChecksumValue:     opts.ChecksumValue,
	}
	b.objects[key] = &memObject{data: prepared.Data, meta: meta}
	out := cloneObjectMeta(meta)
	return &out, nil
}

func (s *MemoryStore) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, *ObjectMeta, error) {
	if err := ValidateBucketName(bucket); err != nil {
		return nil, nil, err
	}
	if err := ValidateKey(key); err != nil {
		return nil, nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return nil, nil, ErrBucketNotFound
	}
	obj, ok := b.objects[key]
	if !ok {
		return nil, nil, ErrObjectNotFound
	}
	meta := cloneObjectMeta(obj.meta)
	return &memoryObjectReader{Reader: bytes.NewReader(obj.data)}, &meta, nil
}

func (s *MemoryStore) HeadObject(_ context.Context, bucket, key string) (*ObjectMeta, error) {
	if err := ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return nil, ErrBucketNotFound
	}
	obj, ok := b.objects[key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	meta := cloneObjectMeta(obj.meta)
	return &meta, nil
}

func (s *MemoryStore) DeleteObject(_ context.Context, bucket, key string) error {
	if err := ValidateBucketName(bucket); err != nil {
		return err
	}
	if err := ValidateKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return ErrBucketNotFound
	}
	if _, ok := b.objects[key]; !ok {
		return ErrObjectNotFound
	}
	delete(b.objects, key)
	return nil
}

func (s *MemoryStore) DeleteObjects(_ context.Context, bucket string, keys []string) ([]string, error) {
	if err := ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return nil, ErrBucketNotFound
	}
	var deleted []string
	for _, key := range keys {
		if err := ValidateKey(key); err != nil {
			return deleted, err
		}
		// A key that was not there is still confirmed: S3 deletes idempotently
		// and reports it as deleted rather than as an error.
		if _, ok := b.objects[key]; ok {
			delete(b.objects, key)
		}
		deleted = append(deleted, key)
	}
	return deleted, nil
}

func (s *MemoryStore) ListObjectsV2(_ context.Context, bucket string, opts ListOptions) (*ListResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return nil, ErrBucketNotFound
	}

	items := make([]ObjectMeta, 0, len(b.objects))
	for key, obj := range b.objects {
		if opts.Prefix != "" && !strings.HasPrefix(key, opts.Prefix) {
			continue
		}
		items = append(items, cloneObjectMeta(obj.meta))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return PaginateObjects(items, opts), nil
}

func (s *MemoryStore) CreateMultipartUpload(_ context.Context, bucket, key string, opts MultipartOptions) (*MultipartUpload, error) {
	if err := ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := ValidateKey(key); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return nil, ErrBucketNotFound
	}
	uploadID, err := NewUploadID()
	if err != nil {
		return nil, err
	}
	upload := MultipartUpload{
		UploadID:  uploadID,
		Bucket:    bucket,
		Key:       key,
		Initiated: time.Now().UTC(),
		Options:   opts.clone(),
	}
	b.multipart[uploadID] = &memMultipart{upload: CloneMultipartUpload(upload), parts: make(map[int]memPart)}
	out := upload
	return &out, nil
}

func (s *MemoryStore) GetMultipartUpload(_ context.Context, uploadID string) (*MultipartUpload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mp, _, ok := s.findMultipart(uploadID)
	if !ok {
		return nil, ErrUploadNotFound
	}
	upload := CloneMultipartUpload(mp.upload)
	return &upload, nil
}

func (s *MemoryStore) UploadPart(_ context.Context, uploadID string, partNumber int, body io.Reader) (*PartInfo, error) {
	if partNumber < 1 || partNumber > 10000 {
		return nil, ErrInvalidPart
	}
	if owned, ok := body.(*OwnedBody); ok {
		data, err := owned.take()
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	etag, data, err := ETagForReader(body)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	mp, _, ok := s.findMultipart(uploadID)
	if !ok {
		return nil, ErrUploadNotFound
	}
	now := time.Now().UTC()
	part := PartInfo{
		PartNumber:   partNumber,
		ETag:         etag,
		Size:         int64(len(data)),
		LastModified: now,
	}
	mp.parts[partNumber] = memPart{info: part, data: data}
	out := part
	return &out, nil
}

func (s *MemoryStore) AbortMultipartUpload(_ context.Context, uploadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mp, bucket, ok := s.findMultipart(uploadID)
	if !ok {
		return ErrUploadNotFound
	}
	delete(bucket.multipart, mp.upload.UploadID)
	return nil
}

func (s *MemoryStore) listParts(_ context.Context, uploadID string) ([]PartInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mp, _, ok := s.findMultipart(uploadID)
	if !ok {
		return nil, ErrUploadNotFound
	}
	nums := make([]int, 0, len(mp.parts))
	for n := range mp.parts {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	out := make([]PartInfo, 0, len(nums))
	for _, n := range nums {
		out = append(out, mp.parts[n].info)
	}
	return out, nil
}

// ListPartsPage returns a marker-paginated page of uploaded parts.
func (s *MemoryStore) ListPartsPage(ctx context.Context, uploadID string, opts ListPartsOptions) (*ListPartsResult, error) {
	parts, err := s.listParts(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	return PaginateParts(parts, opts), nil
}

func (s *MemoryStore) ListParts(ctx context.Context, uploadID string) ([]PartInfo, error) {
	return s.listParts(ctx, uploadID)
}

func (s *MemoryStore) ValidateMultipartUpload(_ context.Context, uploadID, bucket, key string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mp, storedBucket, ok := s.findMultipart(uploadID)
	if !ok || storedBucket.info.Name != bucket || mp.upload.Key != key {
		return ErrNoSuchUpload
	}
	return nil
}

func (s *MemoryStore) Close() error {
	return nil
}

func (s *MemoryStore) ListMultipartUploads(_ context.Context, bucket string, opts MultipartListOptions) (*MultipartListResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return nil, ErrBucketNotFound
	}
	uploads := make([]MultipartUpload, 0, len(b.multipart))
	for _, upload := range b.multipart {
		uploads = append(uploads, CloneMultipartUpload(upload.upload))
	}
	return PaginateMultipartUploads(uploads, opts), nil
}

func (s *MemoryStore) findMultipart(uploadID string) (*memMultipart, *memBucket, bool) {
	for _, b := range s.buckets {
		if mp, ok := b.multipart[uploadID]; ok {
			return mp, b, true
		}
	}
	return nil, nil, false
}

type memoryObjectReader struct{ *bytes.Reader }

func (*memoryObjectReader) Close() error { return nil }

func (*MemoryStore) ComputesPutChecksums() bool { return true }
