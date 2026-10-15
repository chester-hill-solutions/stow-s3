package storage

import (
	"context"
	"io"
)

func CopyObject(ctx context.Context, store Store, req CopyRequest) (*ObjectMeta, error) {
	if conditional, ok := store.(ConditionalCopyStore); ok {
		return conditional.CopyObjectCond(ctx, req)
	}
	if req.Options == (CopyOptions{}) {
		return store.CopyObject(ctx, req.SourceBucket, req.SourceKey, req.DestBucket, req.DestKey)
	}
	if err := ValidateCopyRequest(req); err != nil {
		return nil, err
	}
	source, meta, err := store.GetObject(ctx, req.SourceBucket, req.SourceKey)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	if err := CheckCopySourceConditions(req.Options, meta); err != nil {
		return nil, err
	}
	return store.PutObject(ctx, req.DestBucket, req.DestKey, copySourceReader{Reader: source, size: meta.Size}, PutOptions{
		ContentType:       meta.ContentType,
		Metadata:          meta.Metadata,
		ChecksumAlgorithm: meta.ChecksumAlgorithm,
		ChecksumValue:     meta.ChecksumValue,
	})
}

type copySourceReader struct {
	io.Reader
	size int64
}

func (r copySourceReader) ReadSize() int64 { return r.size }
