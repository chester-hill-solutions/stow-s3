package fs

// The copy operation, which is the one write whose correctness is a question
// about when something happened rather than about what it contains.

import (
	"context"
	"os"
	"time"

	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *FilesystemStore) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (*storage.ObjectMeta, error) {
	return s.CopyObjectCond(ctx, storage.CopyRequest{
		SourceBucket: srcBucket,
		SourceKey:    srcKey,
		DestBucket:   dstBucket,
		DestKey:      dstKey,
	})
}

// CopyObjectCond copies from one captured source record, under one lock.
//
// The read and the write happen together, which is what makes this a single
// transaction. It used to be GetObject then PutObject, and each of those took
// the store lock separately — so a source overwritten in between was copied as
// the bytes of one version carrying the metadata of another.
//
// The conditions are evaluated against the record being copied, under that same
// lock, so a condition naming a version the source has moved on from refuses the
// copy instead of publishing a version the caller never asked for.
func (s *FilesystemStore) CopyObjectCond(_ context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	if err := storage.ValidateCopyRequest(req); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireBucket(req.SourceBucket); err != nil {
		return nil, err
	}
	if err := s.requireBucket(req.DestBucket); err != nil {
		return nil, err
	}
	record, err := s.readObject(req.SourceBucket, req.SourceKey)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrObjectNotFound
		}
		return nil, err
	}
	sourceMeta := record.meta(req.SourceBucket, req.SourceKey)
	if err := storage.CheckCopySourceConditions(req.Options, &sourceMeta); err != nil {
		return nil, err
	}
	recordVersion, err := storage.NewRecordVersion()
	if err != nil {
		return nil, err
	}
	// The bytes are the source record's; only the identity is new, because a
	// copy is a new publication of the same bytes.
	copied := objectRecord{
		RecordVersion:     recordVersion,
		Data:              record.Data,
		ETag:              record.ETag,
		ContentType:       record.ContentType,
		Metadata:          storage.CloneMetadata(record.Metadata),
		ChecksumAlgorithm: record.ChecksumAlgorithm,
		ChecksumValue:     record.ChecksumValue,
		LastModified:      time.Now().UTC(),
	}
	if err := s.writeObject(req.DestBucket, req.DestKey, copied); err != nil {
		return nil, err
	}
	meta := copied.meta(req.DestBucket, req.DestKey)
	return &meta, nil
}
