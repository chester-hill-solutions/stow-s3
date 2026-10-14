package stow

import "github.com/chester-hill-solutions/stow-s3/internal/embedded"

var (
	ErrMutationCommitted            = embedded.ErrMutationCommitted
	ErrBucketNotFound               = embedded.ErrBucketNotFound
	ErrBucketExists                 = embedded.ErrBucketExists
	ErrBucketNotEmpty               = embedded.ErrBucketNotEmpty
	ErrObjectNotFound               = embedded.ErrObjectNotFound
	ErrInvalidBucket                = embedded.ErrInvalidBucket
	ErrInvalidKey                   = embedded.ErrInvalidKey
	ErrPreconditionFailed           = embedded.ErrPreconditionFailed
	ErrConditionalWritesUnsupported = embedded.ErrConditionalWritesUnsupported
)
