package embedded

import (
	"context"
	"io"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type StagedObject interface {
	Commit(context.Context) (Object, error)
	Abort(context.Context) error
}

type StagedWriteStore interface {
	StageObject(context.Context, string, string, io.Reader, PutOptions) (StagedObject, error)
}

type storeWithStaging struct {
	storage.Store
	adapter *storeAdapter
	stager  StagedWriteStore
}

func (s *storeWithStaging) OpenObjectRange(ctx context.Context, bucket, key string, span storage.ObjectRange) (io.ReadCloser, *storage.ObjectMeta, error) {
	return s.adapter.OpenObjectRange(ctx, bucket, key, span)
}

func (s *storeWithStaging) StageObject(ctx context.Context, bucket, key string, body io.Reader, options storage.PutOptions) (storage.StagedObject, error) {
	if (options.IfMatch != "" || options.IfNoneMatch != "") && !supportsConditionalWrites(s.adapter.store) {
		return nil, ErrConditionalWritesUnsupported
	}
	pending, err := s.stager.StageObject(ctx, bucket, key, body, PutOptions{ContentType: options.ContentType, Metadata: storage.CloneMetadata(options.Metadata), IfMatch: options.IfMatch, IfNoneMatch: options.IfNoneMatch})
	if err != nil {
		return nil, translate(err)
	}
	return &stagedObjectAdapter{pending}, nil
}

type stagedObjectAdapter struct{ StagedObject }

func (s *stagedObjectAdapter) Commit(ctx context.Context) (*storage.ObjectMeta, error) {
	object, err := s.StagedObject.Commit(ctx)
	return objectMetaOf(object), translate(err)
}

func (s *stagedObjectAdapter) Abort(ctx context.Context) error {
	return translate(s.StagedObject.Abort(ctx))
}
