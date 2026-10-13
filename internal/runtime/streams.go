package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *Instance) OpenObject(ctx context.Context, bucket, key string) (io.ReadCloser, Object, error) {
	return i.openObject(ctx, bucket, key, nil)
}

func (i *Instance) OpenObjectRange(ctx context.Context, bucket, key string, span storage.ObjectRange) (io.ReadCloser, Object, error) {
	if span.Offset < 0 || span.Length < 0 {
		return nil, Object{}, fmt.Errorf("object range offset and length must not be negative")
	}
	return i.openObject(ctx, bucket, key, &span)
}

func (i *Instance) openObject(ctx context.Context, bucket, key string, span *storage.ObjectRange) (io.ReadCloser, Object, error) {
	if err := i.checkContext(ctx); err != nil {
		return nil, Object{}, err
	}
	if err := i.checkResource(authority.ObjectRead, policy.Object(bucket, key)); err != nil {
		return nil, Object{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return nil, Object{}, err
	}
	reader, meta, err := i.openStoreRange(ctx, bucket, key, span)
	if err != nil {
		return nil, Object{}, err
	}
	return &objectStream{instance: i, context: ctx, bucket: bucket, key: key, reader: reader}, objectFromMeta(meta, nil), nil
}

func (i *Instance) openStoreRange(ctx context.Context, bucket, key string, span *storage.ObjectRange) (io.ReadCloser, *storage.ObjectMeta, error) {
	if ranged, ok := i.store.(storage.RangeStore); ok && span != nil {
		return ranged.OpenObjectRange(ctx, bucket, key, *span)
	}
	reader, meta, err := i.store.GetObject(ctx, bucket, key)
	if err != nil || span == nil {
		return reader, meta, err
	}
	if span.Offset > meta.Size || span.Length > meta.Size-span.Offset {
		_ = reader.Close()
		return nil, nil, fmt.Errorf("object range exceeds object size")
	}
	if _, err := io.CopyN(io.Discard, reader, span.Offset); err != nil {
		_ = reader.Close()
		return nil, nil, err
	}
	return &limitedObjectReader{Reader: io.LimitReader(reader, span.Length), closer: reader}, meta, nil
}

type limitedObjectReader struct {
	io.Reader
	closer io.Closer
}

func (r *limitedObjectReader) Close() error { return r.closer.Close() }

type objectStream struct {
	mu       sync.Mutex
	instance *Instance
	context  context.Context
	bucket   string
	key      string
	reader   io.ReadCloser
	closed   bool
}

func (r *objectStream) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	i := r.instance
	i.mu.Lock()
	defer i.mu.Unlock()
	err := i.checkContext(r.context)
	if err == nil {
		err = i.checkOpen()
	}
	if err == nil {
		err = i.checkResource(authority.ObjectRead, policy.Object(r.bucket, r.key))
	}
	if err != nil {
		_ = r.closeLocked()
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if err != nil {
		_ = r.closeLocked()
	}
	return n, err
}

func (r *objectStream) closeLocked() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.reader.Close()
}

func (r *objectStream) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closeLocked()
}
