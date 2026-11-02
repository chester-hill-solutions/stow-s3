package storage

import "errors"

var (
	ErrBucketNotFound    = errors.New("bucket not found")
	ErrInvalidBucketName = errors.New("invalid bucket name")
	ErrBucketExists      = errors.New("bucket already exists")
	ErrObjectNotFound    = errors.New("object not found")
	ErrInvalidKey        = errors.New("invalid object key")
	ErrInvalidUpload     = errors.New("invalid multipart upload")
	ErrInvalidPart       = errors.New("invalid multipart part")
	// ErrObjectUncertain means the store could not determine whether the object
	// is there. Reporting absence instead loses data: a create-only write over an
	// unreadable object replaces bytes that are still there.
	ErrObjectUncertain = errors.New("object state is uncertain")
	// ErrEntityTooSmall is a non-final part below MinPartSize. It has a name of
	// its own rather than reusing ErrInvalidPart because S3 reports the two
	// differently: one part cannot be found, the other is not allowed to be
	// that small, and a client that has to distinguish them cannot fix a
	// missing part but can fix a small one.
	ErrEntityTooSmall       = errors.New("a non-final multipart part is smaller than the minimum allowed size")
	ErrUploadNotFound       = errors.New("multipart upload not found")
	ErrNoSuchUpload         = errors.New("no such upload")
	ErrPreconditionFailed   = errors.New("precondition failed")
	ErrChecksumMismatch     = errors.New("checksum mismatch")
	ErrMD5Mismatch          = errors.New("Content-MD5 mismatch")
	ErrBucketNotEmpty       = errors.New("bucket not empty")
	ErrMultipartUnsupported = errors.New("this store does not support multipart")
)
