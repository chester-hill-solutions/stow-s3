package embedded

import (
	"context"
	"io"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type ObjectRange = storage.ObjectRange

type StreamingStore interface {
	OpenObject(context.Context, string, string) (io.ReadCloser, Object, error)
}

type RangeStore interface {
	OpenObjectRange(context.Context, string, string, ObjectRange) (io.ReadCloser, Object, error)
}

type StreamingWriteStore interface {
	PutObjectStream(context.Context, string, string, io.Reader, PutOptions) (Object, error)
}

type CopyStore interface {
	CopyObject(context.Context, string, string, string, string) (Object, error)
}

type StreamPutOptions struct {
	Size int64
	PutOptions
}

func (r *Runtime) PutObjectStream(ctx context.Context, bucket, key string, body io.Reader, options StreamPutOptions) (Object, error) {
	object, err := r.inner.PutObjectStream(ctx, bucket, key, body, stowruntime.StreamPutOptions{Size: options.Size, PutOptions: streamPutOptions(options.PutOptions)})
	return objectOf(object), mapError(err)
}

func streamPutOptions(options PutOptions) stowruntime.PutOptions {
	return stowruntime.PutOptions{ContentType: options.ContentType, Metadata: storage.CloneMetadata(options.Metadata), IfMatch: options.IfMatch, IfNoneMatch: options.IfNoneMatch}
}

func (r *Runtime) OpenObject(ctx context.Context, bucket, key string) (io.ReadCloser, Object, error) {
	reader, object, err := r.inner.OpenObject(ctx, bucket, key)
	return mappedObjectStream(reader), objectOf(object), mapError(err)
}

func (r *Runtime) OpenObjectRange(ctx context.Context, bucket, key string, span ObjectRange) (io.ReadCloser, Object, error) {
	reader, object, err := r.inner.OpenObjectRange(ctx, bucket, key, span)
	return mappedObjectStream(reader), objectOf(object), mapError(err)
}

type mappedReader struct{ io.ReadCloser }

func mappedObjectStream(reader io.ReadCloser) io.ReadCloser {
	if reader == nil {
		return nil
	}
	return &mappedReader{reader}
}

func (r *mappedReader) Read(buffer []byte) (int, error) {
	n, err := r.ReadCloser.Read(buffer)
	return n, mapError(err)
}

func (r *mappedReader) Close() error { return mapError(r.ReadCloser.Close()) }

func (a *storeAdapter) OpenObjectRange(ctx context.Context, bucket, key string, span storage.ObjectRange) (io.ReadCloser, *storage.ObjectMeta, error) {
	if ranged, ok := a.store.(RangeStore); ok {
		reader, object, err := ranged.OpenObjectRange(ctx, bucket, key, span)
		return reader, objectMetaOf(object), translate(err)
	}
	reader, meta, err := a.GetObject(ctx, bucket, key)
	if err != nil {
		return reader, meta, err
	}
	if span.Offset < 0 || span.Length < 0 || span.Offset > meta.Size || span.Length > meta.Size-span.Offset {
		_ = reader.Close()
		return nil, nil, io.ErrUnexpectedEOF
	}
	if _, err := io.CopyN(io.Discard, reader, span.Offset); err != nil {
		_ = reader.Close()
		return nil, nil, err
	}
	return &rangeReader{Reader: io.LimitReader(reader, span.Length), closer: reader}, meta, nil
}

type rangeReader struct {
	io.Reader
	closer io.Closer
}

func (r *rangeReader) Close() error { return r.closer.Close() }
