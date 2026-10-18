package fs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

var _ storage.StagedWriteStore = (*FilesystemStore)(nil)

type filesystemStagedObject struct {
	mu          sync.Mutex
	store       *FilesystemStore
	bucket, key string
	options     storage.PutOptions
	prepared    *storage.PreparedPut
	file        *os.File
	size        int64
	etag        string
	consumed    bool
	registered  bool
}

func (s *FilesystemStore) StageObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (storage.StagedObject, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, err
	}
	s.mu.Lock()
	err := s.checkStageOptionsLocked(bucket, key, opts)
	buffered := s.namespace != nil || opts.Guard != nil || opts.RequestKey != ""
	if err == nil {
		s.activeStages++
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	opts.Metadata = storage.CloneMetadata(opts.Metadata)
	if opts.Guard != nil {
		guard := *opts.Guard
		opts.Guard = &guard
	}
	pending := &filesystemStagedObject{store: s, bucket: bucket, key: key, options: opts, registered: true}
	if _, owned := body.(*storage.OwnedBody); buffered || owned {
		prepared, err := storage.PreparePut(body, opts)
		if err != nil {
			_ = pending.Abort(context.Background())
			return nil, err
		}
		pending.prepared = &prepared
		return pending, nil
	}
	return s.stageBody(ctx, body, pending)
}

func (s *FilesystemStore) checkStageOptionsLocked(bucket, key string, opts storage.PutOptions) error {
	if s.closed {
		return errors.New("filesystem store is closed")
	}
	if opts.RequestKey != "" {
		return s.checkSaveRequest(bucket, key, opts)
	}
	return s.requireBucket(bucket)
}

func (s *FilesystemStore) stageFile(ctx context.Context, body io.Reader, p *filesystemStagedObject) (storage.StagedObject, error) {
	file, err := s.createStageFile()
	if err != nil {
		_ = p.Abort(context.Background())
		return nil, err
	}
	p.file = file
	opts, size, etag, err := storage.PreparePutTo(ctx, body, file, p.options)
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		_ = p.Abort(context.Background())
		return nil, err
	}
	p.options, p.size, p.etag = opts, size, etag
	return p, nil
}

func (s *FilesystemStore) createStageFile() (*os.File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("filesystem store is closed")
	}
	return os.CreateTemp(s.dataDir, ".tmp-body-*")
}

func (p *filesystemStagedObject) Abort(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consumed = true
	p.prepared = nil
	return p.finish()
}

func (p *filesystemStagedObject) finish() error {
	err := p.removeFile()
	if p.registered {
		p.registered = false
		p.store.mu.Lock()
		p.store.activeStages--
		p.store.mu.Unlock()
	}
	return err
}

func (p *filesystemStagedObject) removeFile() error {
	if p.file == nil {
		return nil
	}
	file := p.file
	p.file = nil
	return errors.Join(file.Close(), os.Remove(file.Name()))
}

func (p *filesystemStagedObject) Commit(ctx context.Context) (*storage.ObjectMeta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed {
		return nil, errors.New("filesystem object is no longer staged")
	}
	p.consumed = true
	defer func() { p.prepared = nil; _ = p.finish() }()
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s := p.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkStageOptionsLocked(p.bucket, p.key, p.options); err != nil {
		return nil, err
	}
	if s.namespace != nil || p.options.Guard != nil || p.options.RequestKey != "" {
		return p.commitBufferedLocked()
	}
	return p.commitStreamLocked()
}

func (p *filesystemStagedObject) commitBufferedLocked() (*storage.ObjectMeta, error) {
	prepared := p.prepared
	if prepared == nil {
		data, err := io.ReadAll(p.file)
		if err != nil {
			return nil, err
		}
		prepared = &storage.PreparedPut{Data: data, Options: p.options}
	}
	return p.store.putBufferedLocked(p.bucket, p.key, storage.NewOwnedBody(prepared.Data), prepared.Options)
}

func (p *filesystemStagedObject) commitStreamLocked() (*storage.ObjectMeta, error) {
	s := p.store
	s.usageRevision++
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}
	if err := s.checkRecoveryHoldsLocked(p.bucket, p.key); err != nil {
		return nil, err
	}
	existing, _, err := s.readExistingForWriteLocked(p.bucket, p.key, false)
	if err != nil {
		return nil, err
	}
	if err := storage.CheckWritePreconditions(p.options, existing); err != nil {
		return nil, err
	}
	version, err := storage.NewRecordVersion()
	if err != nil {
		return nil, err
	}
	record := objectRecord{RecordVersion: version, ContentType: p.options.ContentType, Metadata: p.options.Metadata,
		ETag: p.etag, ChecksumAlgorithm: p.options.ChecksumAlgorithm, ChecksumValue: p.options.ChecksumValue, LastModified: time.Now().UTC(), payloadSize: p.size}
	var body io.Reader = p.file
	if p.prepared != nil {
		record.Data = p.prepared.Data
		record.ETag = p.prepared.ETag
		record.ChecksumAlgorithm = p.prepared.Options.ChecksumAlgorithm
		record.ChecksumValue = p.prepared.Options.ChecksumValue
		body = bytes.NewReader(record.Data)
	}
	if err := s.writeObjectStream(p.bucket, p.key, record, body); err != nil {
		return nil, fmt.Errorf("publish staged object: %w", err)
	}
	meta := record.meta(p.bucket, p.key)
	return &meta, nil
}
