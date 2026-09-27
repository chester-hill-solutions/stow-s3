package storage

// The memory backend's copy, which is the one write whose correctness is a question
// about when something happened rather than about what it contains.

import (
	"context"
	"time"
)

func (s *MemoryStore) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (*ObjectMeta, error) {
	return s.CopyObjectCond(ctx, CopyRequest{
		SourceBucket: srcBucket,
		SourceKey:    srcKey,
		DestBucket:   dstBucket,
		DestKey:      dstKey,
	})
}

// CopyObjectCond copies from one captured source version, under one lock.
//
// The capture and the commit happen together, which is what makes the copy a
// single transaction rather than a read followed by a write. It used to be
// GetObject then PutObject: each took the store lock in turn, so between them
// any other writer could replace the source, and the bytes that arrived at the
// destination were then paired with metadata read before them — a body from one
// version of an object and a content type and user metadata from another.
//
// A stored buffer is never modified in place — a write publishes a new one — so
// the captured slice is immutable and the destination can share it instead of
// copying it. The destination gets its own version, because it is a new
// publication of the same bytes.
func (s *MemoryStore) CopyObjectCond(_ context.Context, req CopyRequest) (*ObjectMeta, error) {
	if err := ValidateCopyRequest(req); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	src, ok := s.buckets[req.SourceBucket]
	if !ok {
		return nil, ErrBucketNotFound
	}
	source, ok := src.objects[req.SourceKey]
	if !ok {
		return nil, ErrObjectNotFound
	}
	sourceMeta := cloneObjectMeta(source.meta)
	// Evaluated against the version being copied, under this lock: a condition
	// naming an ETag the source has since moved on from refuses the copy rather
	// than quietly copying a version the caller never asked for.
	if err := CheckCopySourceConditions(req.Options, &sourceMeta); err != nil {
		return nil, err
	}
	dst, ok := s.buckets[req.DestBucket]
	if !ok {
		return nil, ErrBucketNotFound
	}
	versionID, err := NewRecordVersion()
	if err != nil {
		return nil, err
	}
	meta := ObjectMeta{
		Bucket:            req.DestBucket,
		Key:               req.DestKey,
		VersionID:         versionID,
		Size:              sourceMeta.Size,
		ETag:              sourceMeta.ETag,
		ContentType:       sourceMeta.ContentType,
		LastModified:      time.Now().UTC(),
		Metadata:          CloneMetadata(sourceMeta.Metadata),
		ChecksumAlgorithm: sourceMeta.ChecksumAlgorithm,
		ChecksumValue:     sourceMeta.ChecksumValue,
	}
	dst.objects[req.DestKey] = &memObject{data: source.data, meta: meta}
	out := cloneObjectMeta(meta)
	return &out, nil
}
