package storage

import "time"

// The rules every backend applies when a multipart upload finishes. They live
// here rather than in each store because they are one rule stated four times
// otherwise, and a rule that differs by backend decides the outcome of the
// request based on which store happens to be underneath it.

// MinPartSize is the smallest a non-final part may be, in bytes.
//
// S3's rule, and the reason multipart exists at all: a client that could append
// a hundred bytes at a time would make one request per hundred bytes of an
// object, so a part below this size is only permitted as the last one, where
// nothing can be appended after it.
const MinPartSize = 5 * 1024 * 1024

// ValidateMinPartSizes refuses a completion whose non-final parts are too small.
//
// sizes are the stored sizes of the selected parts, in the order they will be
// concatenated. The final part is exempt: it is the one nothing can be appended
// to, and exempting it is what makes a small object expressible as a single
// part.
//
// A backend calls this with the sizes it has already read for the parts it is
// about to assemble, which means the decision is made from stored state rather
// than from the request: a client cannot talk its way past it by describing a
// part as bigger than it is.
func ValidateMinPartSizes(sizes []int64) error {
	for i, size := range sizes {
		if i == len(sizes)-1 {
			return nil
		}
		if size < MinPartSize {
			return ErrEntityTooSmall
		}
	}
	return nil
}

// MultipartPutOptions renders a multipart initiation's options as the write
// options they describe.
//
// It is for the one thing a completion needs them as PutOptions rather than as
// MultipartOptions: verifying an integrity claim. A client that declared a
// checksum for the object it is assembling has made the same claim a single
// write would, about the same bytes, and it is checked the same way.
func MultipartPutOptions(opts MultipartOptions) PutOptions {
	return PutOptions{
		ContentType:       opts.ContentType,
		Metadata:          CloneMetadata(opts.Metadata),
		ChecksumAlgorithm: opts.ChecksumAlgorithm,
		ChecksumValue:     opts.ChecksumValue,
	}
}

// Completion is what a validated completion produced, before it is published.
//
// It exists so the three backends that materialize an object build its metadata
// the same way. A completion is a commit: the bytes are assembled, the ETag is
// the one S3 would return, and the properties are the ones fixed at initiation.
// Anything a backend adds here is something a client would have to discover
// from one backend but not another.
type Completion struct {
	Upload      MultipartUpload
	ETag        string
	Size        int64
	VersionID   string
	CompletedAt time.Time
}

// ObjectMeta renders a completion as the metadata of the object it publishes.
//
// The properties come from the upload rather than from the parts, so an upload
// initiated with a content type completes into an object carrying it. A
// completion that dropped them published an object indistinguishable from one
// written with PutObject and no options at all, which is how a Content-Type
// supplied at initiation went missing.
func (c Completion) ObjectMeta() ObjectMeta {
	opts := c.Upload.Options
	return ObjectMeta{
		Bucket:            c.Upload.Bucket,
		Key:               c.Upload.Key,
		VersionID:         c.VersionID,
		Size:              c.Size,
		ETag:              c.ETag,
		ContentType:       opts.ContentType,
		LastModified:      c.CompletedAt.UTC(),
		Metadata:          CloneMetadata(opts.Metadata),
		ChecksumAlgorithm: NormalizeChecksumAlgorithm(opts.ChecksumAlgorithm),
		ChecksumValue:     opts.ChecksumValue,
	}
}
