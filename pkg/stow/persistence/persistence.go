package persistence

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrNotFound           = errors.New("persistence: object not found")
	ErrBucketNotFound     = errors.New("persistence: bucket not found")
	ErrBucketExists       = errors.New("persistence: bucket already exists")
	ErrBucketNotEmpty     = errors.New("persistence: bucket is not empty")
	ErrPreconditionFailed = errors.New("persistence: precondition failed")
	ErrInvalidKey         = errors.New("persistence: invalid key")
	ErrInvalidToken       = errors.New("persistence: invalid continuation token")
	ErrInvalidResult      = errors.New("persistence: adapter returned an invalid result")
)

type Meta struct {
	Key               string
	Size              int64
	ETag              string
	Modified          time.Time
	ContentType       string
	Metadata          map[string]string
	ChecksumAlgorithm string
	ChecksumValue     string
}

type Object struct {
	Meta Meta
	Body io.ReadCloser
}

type PutOptions struct {
	ContentType       string
	Metadata          map[string]string
	ChecksumAlgorithm string
	ChecksumValue     string
}

type Condition struct {
	IfMatch  string
	IfAbsent bool
}

type ConditionalPutOptions struct {
	PutOptions
	Condition Condition
}

type ListOptions struct {
	Prefix            string
	Delimiter         string
	ContinuationToken string
	StartAfter        string
	MaxKeys           int
}

type ListResult struct {
	Objects               []Meta
	CommonPrefixes        []string
	IsTruncated           bool
	NextContinuationToken string
}

type Adapter interface {
	Get(ctx context.Context, bucket, key string) (*Object, error)
	Put(ctx context.Context, bucket, key string, body io.Reader, options PutOptions) (*Meta, error)
	ConditionalPut(ctx context.Context, bucket, key string, body io.Reader, options ConditionalPutOptions) (*Meta, error)
	List(ctx context.Context, bucket string, options ListOptions) (*ListResult, error)
	Delete(ctx context.Context, bucket, key string) error
}

type Header interface {
	Head(ctx context.Context, bucket, key string) (*Meta, error)
}

type BucketAdmin interface {
	CreateBucket(ctx context.Context, name string) error
	DeleteBucket(ctx context.Context, name string) error
	ListBuckets(ctx context.Context) ([]string, error)
}
