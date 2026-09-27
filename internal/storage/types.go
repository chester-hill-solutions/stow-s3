package storage

import (
	"time"
)

// ObjectMeta describes a stored object.
type ObjectMeta struct {
	Bucket            string
	Key               string
	VersionID         string
	Size              int64
	ETag              string
	ContentType       string
	LastModified      time.Time
	Metadata          map[string]string
	ChecksumAlgorithm string
	ChecksumValue     string
}

// BucketInfo describes a bucket.
type BucketInfo struct {
	Name         string
	CreationDate time.Time
}

// PutOptions configures object writes.
type PutOptions struct {
	ContentType       string
	Metadata          map[string]string
	ChecksumAlgorithm string
	ChecksumValue     string
	IfMatch           string
	IfNoneMatch       string
}

// ListOptions configures object listing.
type ListOptions struct {
	Prefix            string
	Delimiter         string
	ContinuationToken string
	MaxKeys           int
	StartAfter        string
}

// ListResult is the result of ListObjectsV2.
type ListResult struct {
	Objects               []ObjectMeta
	CommonPrefixes        []string
	IsTruncated           bool
	ContinuationToken     string
	NextContinuationToken string
	KeyCount              int
}

// MultipartUpload describes an in-progress multipart upload.
type MultipartUpload struct {
	UploadID  string
	Bucket    string
	Key       string
	Initiated time.Time
	// Options are the object properties that were fixed when the upload was
	// initiated. They are part of the upload rather than of the parts, because
	// S3 fixes them once: no part may change them, and the object that
	// completion publishes carries exactly these.
	//
	// This is why the descriptor carries them rather than only the store
	// remembering them privately. A caller reconciling in-flight uploads — or a
	// caller asking what an upload will publish — can only answer from the
	// upload, and a store that had to keep the properties to itself could not
	// answer at all.
	Options MultipartOptions
}

// MultipartOptions are the object properties a multipart upload fixes at
// initiation.
//
// It is the multipart half of PutOptions, deliberately: these are the same
// creation-time properties a single write accepts, minus the two that are
// properties of a *write* rather than of an object. If-Match and If-None-Match
// name the object a write replaces, and there is no object yet at initiation
// for them to name, so S3 does not accept them here and neither does this.
type MultipartOptions struct {
	ContentType       string
	Metadata          map[string]string
	ChecksumAlgorithm string
	ChecksumValue     string
}

// clone returns an independent copy, so a stored upload cannot be changed
// through a map the caller kept.
func (o MultipartOptions) clone() MultipartOptions {
	o.Metadata = CloneMetadata(o.Metadata)
	return o
}

// CloneMultipartUpload returns an independent copy of an upload descriptor,
// including its metadata map.
func CloneMultipartUpload(upload MultipartUpload) MultipartUpload {
	upload.Options = upload.Options.clone()
	return upload
}

type MultipartListOptions struct {
	Prefix         string
	Delimiter      string
	KeyMarker      string
	UploadIDMarker string
	MaxUploads     int
}

type MultipartListResult struct {
	Uploads            []MultipartUpload
	Prefix             string
	Delimiter          string
	KeyMarker          string
	UploadIDMarker     string
	NextKeyMarker      string
	NextUploadIDMarker string
	MaxUploads         int
	IsTruncated        bool
}

// ListPartsOptions configures a page of uploaded parts.
type ListPartsOptions struct {
	PartNumberMarker int
	MaxParts         int
}

// ListPartsResult describes one page of uploaded parts.
type ListPartsResult struct {
	Parts                []PartInfo
	PartNumberMarker     int
	NextPartNumberMarker int
	MaxParts             int
	IsTruncated          bool
}

// PartInfo describes a single uploaded part.
type PartInfo struct {
	PartNumber   int
	ETag         string
	Size         int64
	LastModified time.Time
}
