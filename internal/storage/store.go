package storage

import (
	"context"
	"io"
)

// Store is the core object storage interface: the object model, and nothing else.
//
// Multipart is deliberately not a member. It is how one protocol happens to
// stream an object larger than memory, and requiring it of every store put that
// protocol's shape into the object model. It is the optional interface below.
type Store interface {
	CreateBucket(ctx context.Context, name string) error
	DeleteBucket(ctx context.Context, name string) error
	HeadBucket(ctx context.Context, name string) (*BucketInfo, error)
	ListBuckets(ctx context.Context) ([]BucketInfo, error)

	PutObject(ctx context.Context, bucket, key string, body io.Reader, opts PutOptions) (*ObjectMeta, error)
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *ObjectMeta, error)
	HeadObject(ctx context.Context, bucket, key string) (*ObjectMeta, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	// DeleteObjects deletes each key and returns the keys it confirmed deleted, in
	// request order.
	//
	// A key that was not there counts as confirmed: S3 deletes idempotently and
	// reports a missing key as deleted rather than as an error, so it appears in
	// the returned slice and in the response's Deleted entries. Only a key whose
	// delete actually failed is left out.
	//
	// On failure it returns the keys confirmed before the failure alongside the
	// error, so a caller retrying the remainder can skip what already succeeded.
	DeleteObjects(ctx context.Context, bucket string, keys []string) ([]string, error)
	// CopyObject publishes the destination key from exactly one version of the
	// source.
	//
	// "One version" is the whole contract, and it is what makes a copy usable:
	// the bytes and the metadata copied always describe the same object, so a
	// copy taken while the source is being overwritten is one complete version
	// rather than a body from one write and a content type from another. A copy
	// composed of a read and a later write can only promise that if nothing
	// changed in between, which is a promise about timing rather than about the
	// result.
	//
	// The version is chosen when the copy runs, so a source that is deleted
	// first may fail with ErrObjectNotFound and a source that is overwritten
	// first yields the newer complete version. Copying a key onto itself is
	// allowed and publishes a new version of it.
	//
	// The destination's own preconditions are not consulted: a copy overwrites.
	// Source preconditions, when a caller has them, are ConditionalCopyStore's
	// business.
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (*ObjectMeta, error)
	ListObjectsV2(ctx context.Context, bucket string, opts ListOptions) (*ListResult, error)

	Close() error
}

// MultipartStore is the optional extension: a store that can stream an object
// larger than memory.
//
// It is whole rather than write-only because a consumer that reconciles in-flight
// uploads when it opens must be able to ask what is in flight. A write-only
// implementation would force the reader to invent that answer.
type MultipartStore interface {
	CreateMultipartUpload(ctx context.Context, bucket, key string, opts MultipartOptions) (*MultipartUpload, error)
	GetMultipartUpload(ctx context.Context, uploadID string) (*MultipartUpload, error)
	UploadPart(ctx context.Context, uploadID string, partNumber int, body io.Reader) (*PartInfo, error)
	CompleteMultipartUpload(ctx context.Context, uploadID string, parts []PartInfo) (*ObjectMeta, error)
	AbortMultipartUpload(ctx context.Context, uploadID string) error
	ListParts(ctx context.Context, uploadID string) ([]PartInfo, error)
	ValidateMultipartUpload(ctx context.Context, uploadID, bucket, key string) error
	ListMultipartUploads(ctx context.Context, bucket string, opts MultipartListOptions) (*MultipartListResult, error)
}

// CopyOptions carries the preconditions a caller may attach to the source of a
// copy.
//
// They are the source-side twin of PutOptions.IfMatch and IfNoneMatch, and they
// mean the same thing: the copy is refused unless the source version being
// copied is the one the caller asked for.
type CopyOptions struct {
	SourceIfMatch     string
	SourceIfNoneMatch string
}

// CopyRequest names a copy: which object, to where, under which conditions.
//
// It is a struct rather than five positional parameters because the parameters
// come in pairs — a bucket and a key are not separable — and a signature that
// takes two adjacent strings invites swapping them. A copy that swapped its
// source and destination would still typecheck.
type CopyRequest struct {
	SourceBucket string
	SourceKey    string
	DestBucket   string
	DestKey      string
	Options      CopyOptions
}

// Copy returns the request as its two halves, for a store that validates each
// pair separately.
func (r CopyRequest) Source() (string, string) { return r.SourceBucket, r.SourceKey }
func (r CopyRequest) Destination() (string, string) {
	return r.DestBucket, r.DestKey
}

// HasSourceConditions reports whether the request carries any, so a caller can
// tell "no conditions" from "conditions that happen to be empty".
func (r CopyRequest) HasSourceConditions() bool {
	return r.Options.SourceIfMatch != "" || r.Options.SourceIfNoneMatch != ""
}

// ConditionalCopyStore is the optional extension: a copy that can refuse to
// publish anything unless the source version satisfies the caller's conditions.
//
// It is optional, and separately so, because this is the one case where two
// implementations can differ in a way a client can observe and cannot fix. A
// caller that checked a condition itself and then called CopyObject has asked
// "copy the version whose ETag I read"; a store that re-reads the source cannot
// honour that, because between the check and the copy the source may have moved
// on, and the copy would then publish a version the caller never asked about and
// never saw. So a store that implements this evaluates the conditions against
// the same captured version whose bytes it copies, and a store that does not
// implement it can only promise that the copy is internally coherent.
type ConditionalCopyStore interface {
	CopyObjectCond(ctx context.Context, req CopyRequest) (*ObjectMeta, error)
}
