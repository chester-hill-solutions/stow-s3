package stow

import (
	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type (
	ObjectRange         = storage.ObjectRange
	StreamingStore      = embedded.StreamingStore
	RangeStore          = embedded.RangeStore
	StreamingWriteStore = embedded.StreamingWriteStore
	CopyStore           = embedded.CopyStore
	StreamPutOptions    = embedded.StreamPutOptions
)
