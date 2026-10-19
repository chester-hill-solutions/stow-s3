package fs

import (
	"context"
	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
	"io"
	"os"
	"time"
)

func (s *FilesystemStore) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	pending, err := s.StageObject(ctx, bucket, key, body, opts)
	if err != nil {
		return nil, err
	}
	defer pending.Abort(context.Background())
	return pending.Commit(ctx)
}

func (s *FilesystemStore) putBufferedLocked(bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	s.usageRevision++
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}
	if err := s.checkRecoveryHoldsLocked(bucket, key); err != nil {
		return nil, err
	}
	var prepared storage.PreparedPut
	if opts.RequestKey != "" {
		var receipt storage.SaveReceipt
		var found bool
		var err error
		prepared, receipt, found, err = s.prepareSaveLocked(bucket, key, body, opts)
		if err != nil || found {
			return receipt.Meta, err
		}
		opts = prepared.Options
	}

	if _, err := os.Stat(s.bucketDir(bucket)); os.IsNotExist(err) {
		return nil, storage.ErrBucketNotFound
	}
	existing, existingData, err := s.readExistingForWriteLocked(bucket, key, opts.Guard != nil)
	if err != nil {
		return nil, err
	}
	if err := storage.CheckWritePreconditions(opts, existing); err != nil {
		return nil, err
	}
	if err := storage.CheckWriteGuard(opts.Guard, existing, existingData); err != nil {
		return nil, err
	}

	if opts.RequestKey == "" {
		prepared, err = storage.PreparePut(body, opts)
		if err != nil {
			return nil, err
		}
		opts = prepared.Options
	}
	recordVersion, err := storage.NewRecordVersion()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := objectRecord{
		RecordVersion:     recordVersion,
		Data:              prepared.Data,
		ContentType:       opts.ContentType,
		Metadata:          opts.Metadata,
		ETag:              prepared.ETag,
		ChecksumAlgorithm: opts.ChecksumAlgorithm,
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
	return s.readExistingForWriteLocked(bucket, key, true)
}

func (s *FilesystemStore) readExistingForWriteLocked(bucket, key string, needBody bool) (*storage.ObjectMeta, []byte, error) {
	objPath := s.objectPath(bucket, key)
	var existing *storage.ObjectMeta
	var existingData []byte
	if _, statErr := os.Stat(objPath); statErr == nil {
		read := s.readObjectHeader
		if needBody {
			read = s.readObject
		}
		record, readErr := read(bucket, key)
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

func (s *FilesystemStore) prepareSaveLocked(bucket, key string, body io.Reader, opts storage.PutOptions) (storage.PreparedPut, storage.SaveReceipt, bool, error) {
	if err := s.checkSaveRequest(bucket, key, opts); err != nil {
		return storage.PreparedPut{}, storage.SaveReceipt{}, false, err
	}
	prepared, err := storage.PreparePut(body, opts)
	if err != nil {
		return storage.PreparedPut{}, storage.SaveReceipt{}, false, err
	}
	receipt, found, err := s.replaySaveLocked(bucket, key, prepared.Data, prepared.Options)
	return prepared, receipt, found, err
}

func (*FilesystemStore) ComputesPutChecksums() bool { return true }
