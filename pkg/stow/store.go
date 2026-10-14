package stow

import "github.com/chester-hill-solutions/stow-s3/internal/embedded"

type (
	Store                 = embedded.Store
	ConditionalWriteStore = embedded.ConditionalWriteStore
	MultipartStore        = embedded.MultipartStore
	MultipartOptions      = embedded.MultipartOptions
	MultipartUpload       = embedded.MultipartUpload
	Part                  = embedded.Part
)
