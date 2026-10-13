package storage

import (
	"context"
	"io"
)

type StagedObject interface {
	Commit(context.Context) (*ObjectMeta, error)
	Abort(context.Context) error
}

type StagedWriteStore interface {
	StageObject(context.Context, string, string, io.Reader, PutOptions) (StagedObject, error)
}
