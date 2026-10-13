package storage

import (
	"context"
	"io"
)

type ObjectRange struct {
	Offset int64
	Length int64
}

type RangeStore interface {
	OpenObjectRange(context.Context, string, string, ObjectRange) (io.ReadCloser, *ObjectMeta, error)
}
