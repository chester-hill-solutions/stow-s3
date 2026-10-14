package fs

// The copy operation, which is the one write whose correctness is a question
// about when something happened rather than about what it contains.

import (
	"context"
	"fmt"
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

// CopyObjectCond checks conditions and copies one captured version under the same lock.
func (s *FilesystemStore) CopyObjectCond(_ context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	if err := storage.ValidateCopyRequest(req); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usageRevision++
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}

	if err := s.requireBucket(req.SourceBucket); err != nil {
		return nil, err
	}
	if err := s.requireBucket(req.DestBucket); err != nil {
		return nil, err
	}
	reader, record, err := openObjectRecord(s.objectPath(req.SourceBucket, req.SourceKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrObjectNotFound
		}
		return nil, err
	}
	defer reader.Close()
	if record.Key != "" && record.Key != req.SourceKey {
		return nil, fmt.Errorf("object path digest collision: stored key does not match requested key")
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
		payloadSize:       record.payloadSize,
		ETag:              record.ETag,
		ContentType:       record.ContentType,
		Metadata:          storage.CloneMetadata(record.Metadata),
		ChecksumAlgorithm: record.ChecksumAlgorithm,
		ChecksumValue:     record.ChecksumValue,
		LastModified:      time.Now().UTC(),
	}
	// The destination of a copy is an object write, so a hold on it refuses the
	// copy rather than letting other bytes take its place.
	if err := s.checkRecoveryHoldsLocked(req.DestBucket, req.DestKey); err != nil {
		return nil, err
	}
	if err := s.writeObjectStream(req.DestBucket, req.DestKey, copied, reader); err != nil {
		return nil, err
	}
	meta := copied.meta(req.DestBucket, req.DestKey)
	return &meta, nil
}
