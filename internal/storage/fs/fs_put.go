package fs

import (
	"bytes"
	"context"
	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
	"io"
	"os"
	"time"
)

func (s *FilesystemStore) PutObject(_ context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}
	if err := s.checkRecoveryHoldsLocked(bucket, key); err != nil {
		return nil, err
	}
	if opts.RequestKey != "" {
		var receipt storage.SaveReceipt
		var found bool
		var err error
		body, receipt, found, err = s.saveBodyLocked(bucket, key, body, opts)
		if err != nil || found {
			return receipt.Meta, err
		}
	}

	if _, err := os.Stat(s.bucketDir(bucket)); os.IsNotExist(err) {
		return nil, storage.ErrBucketNotFound
	}
	existing, existingData, err := s.readExistingLocked(bucket, key)
	if err != nil {
		return nil, err
	}
	if err := storage.CheckWritePreconditions(opts, existing); err != nil {
		return nil, err
	}
	if err := storage.CheckWriteGuard(opts.Guard, existing, existingData); err != nil {
		return nil, err
	}

	etag, data, err := storage.ETagForReader(body)
	if err != nil {
		return nil, err
	}
	if err := storage.VerifyChecksum(opts, data); err != nil {
		return nil, err
	}
	recordVersion, err := storage.NewRecordVersion()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := objectRecord{
		RecordVersion:     recordVersion,
		Data:              data,
		ContentType:       opts.ContentType,
		Metadata:          storage.CloneMetadata(opts.Metadata),
		ETag:              etag,
		ChecksumAlgorithm: storage.NormalizeChecksumAlgorithm(opts.ChecksumAlgorithm),
		ChecksumValue:     opts.ChecksumValue,
		LastModified:      now,
	}
	return s.publishPutLocked(bucket, key, record, opts)
}

func (s *FilesystemStore) publishPutLocked(bucket, key string, record objectRecord, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	if opts.RequestKey != "" {
		return s.commitSaveLocked(bucket, key, record, opts)
	}
	if err := s.writeObject(bucket, key, record); err != nil {
		return nil, err
	}
	meta := record.meta(bucket, key)
	return &meta, nil
}

func (s *FilesystemStore) readExistingLocked(bucket, key string) (*storage.ObjectMeta, []byte, error) {
	objPath := s.objectPath(bucket, key)
	var existing *storage.ObjectMeta
	var existingData []byte
	if _, statErr := os.Stat(objPath); statErr == nil {
		record, readErr := s.readObject(bucket, key)
		if readErr != nil {
			return nil, nil, readErr
		}
		existingMeta := record.meta(bucket, key)
		existing = &existingMeta
		existingData = record.Data
	} else if !os.IsNotExist(statErr) {
		return nil, nil, statErr
	}
	return existing, existingData, nil
}

func (s *FilesystemStore) saveBodyLocked(bucket, key string, body io.Reader, opts storage.PutOptions) (io.Reader, storage.SaveReceipt, bool, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, storage.SaveReceipt{}, false, err
	}
	receipt, found, err := s.replaySaveLocked(bucket, key, data, opts)
	return bytes.NewReader(data), receipt, found, err
}
