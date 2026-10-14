package runtime

import (
	"context"
	"fmt"
	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"io"
)

func (i *Instance) putObjectReaderLocked(ctx context.Context, write stagedWriteRequest) (Object, error) {
	bucket, key, body, requestedSize, options := write.bucket, write.key, write.body, write.size, write.options
	oldSize, exists, err := i.objectSize(ctx, bucket, key)
	if err != nil {
		return Object{}, err
	}
	if !exists && !i.objectQuotaFits(objectTarget(bucket, key), 1) {
		return Object{}, ErrQuotaExceeded
	}
	if !i.bytesQuotaFits(oldSize, requestedSize) {
		return Object{}, ErrQuotaExceeded
	}
	target := objectTarget(bucket, key)
	meta, err := i.store.PutObject(ctx, bucket, key, body, streamStorageOptions(options))
	if !i.finishPut(target, oldSize, requestedSize, exists, err) {
		return Object{}, err
	}
	return objectFromMeta(meta, nil), err
}

type StreamPutOptions struct {
	Size int64
	PutOptions
}

func (i *Instance) PutObjectStream(ctx context.Context, bucket, key string, body io.Reader, stream StreamPutOptions) (Object, error) {
	size, options := stream.Size, stream.PutOptions
	if size < 0 || body == nil {
		return Object{}, fmt.Errorf("stream size must be non-negative and reader is required")
	}
	if err := i.checkContext(ctx); err != nil {
		return Object{}, err
	}
	if err := i.checkResource(authority.ObjectWrite, policy.Object(bucket, key)); err != nil {
		return Object{}, err
	}
	var verified io.Reader = &sizedObjectReader{reader: body, remaining: size}
	if owned, ok := body.(*storage.OwnedBody); ok {
		if int64(owned.Len()) != size {
			return Object{}, fmt.Errorf("owned body does not match declared size")
		}
		verified = owned
	}
	i.mu.Lock()
	if err := i.checkMutationLocked(ctx); err != nil {
		i.mu.Unlock()
		return Object{}, err
	}
	if staged, ok := i.store.(storage.StagedWriteStore); ok {
		epoch := i.storeEpoch
		i.mu.Unlock()
		return i.putStagedObject(ctx, staged, stagedWriteRequest{bucket: bucket, key: key, body: verified, size: size, options: options, epoch: epoch})
	}
	defer i.mu.Unlock()
	return i.putObjectReaderLocked(ctx, stagedWriteRequest{bucket: bucket, key: key, body: verified, size: size, options: options})
}

type sizedObjectReader struct {
	reader    io.Reader
	remaining int64
}

func (r *sizedObjectReader) ReadSize() int64 { return r.remaining }

func (r *sizedObjectReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var extra [1]byte
		n, err := r.reader.Read(extra[:])
		if n > 0 {
			return 0, fmt.Errorf("stream exceeds declared size")
		}
		return 0, err
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	n, err := r.reader.Read(buffer)
	r.remaining -= int64(n)
	if err == io.EOF && r.remaining > 0 {
		err = fmt.Errorf("stream is shorter than declared size")
	}
	return n, err
}
