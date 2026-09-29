package runthrough

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// Client reads and writes objects against upstream S3-compatible storage.
type Client interface {
	HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error)
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error)
	PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (string, error)
	DeleteObject(ctx context.Context, bucket, key, ifMatch string) error
	ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error)
}

// S3Client implements Client with the AWS SDK for Go v2.
type S3Client struct {
	s3     *s3.Client
	region string
}

// NewS3Client builds an upstream client for a custom S3-compatible endpoint.
func NewS3Client(cfg UpstreamConfig) (*S3Client, error) {
	if err := validateUpstreamEndpoint(cfg); err != nil {
		return nil, err
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	// The config is built literally rather than through config.LoadDefaultConfig.
	//
	// LoadDefaultConfig resolves the entire default AWS chain on top of whatever it
	// is given: ~/.aws/config, ~/.aws/credentials, SSO, web-identity token files,
	// and the EC2 instance metadata provider. Region and credentials are both
	// overridden below, and the endpoint is set on the client, so most of that chain
	// turned out to be inert — a shared config declaring another region still signed
	// for us-east-1, and s3_use_accelerate_endpoint did not redirect the request.
	// Those two are asserted as invariants in upstream_config_test.go.
	//
	// Reading the chain was not inert, though. Naming a profile stow never asked for
	// made this function fail, so whether stow could reach upstream storage at all
	// depended on the machine's AWS configuration — and UpstreamConfig is documented
	// as coming from a fixed list of STOW_*/S3_*/AWS_* environment variables and
	// nothing else. The env vars are read where they are documented; this stops the
	// machine from having an opinion.
	awsCfg := aws.Config{
		HTTPClient:  confinedUpstreamClient(),
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		// Path-style unless the configuration asks for virtual-hosted. This was
		// hard-coded, so a provider that only serves bucket-in-host addressing
		// was unreachable and the failure was a DNS lookup with nothing in
		// stow's output to explain it.
		//
		// The SDK derives the virtual-hosted hostname from BaseEndpoint, so
		// setting UsePathStyle false is only meaningful alongside an explicit
		// endpoint; without one the SDK uses the region endpoint, which is
		// already virtual-hosted and would be overridden by asking for
		// path-style. Deriving the style from the configuration rather than from
		// whether an endpoint happens to be set keeps the two from disagreeing,
		// which is the same defect the client-side forcePathStyle/baseHost
		// divergence has.
		o.UsePathStyle = !cfg.UseVirtualHosted()
	})

	return &S3Client{s3: client, region: region}, nil
}

func (c *S3Client) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, mapUpstreamError(err)
	}
	return headOutputToMeta(bucket, key, out)
}

func (c *S3Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, nil, mapUpstreamError(err)
	}
	meta, err := getOutputToMeta(bucket, key, out)
	if err != nil {
		out.Body.Close()
		return nil, nil, err
	}
	return out.Body, meta, nil
}

func (c *S3Client) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (string, error) {
	if body == nil {
		return "", errors.New("upstream put body is nil")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	}
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	// Preconditions are forwarded, not dropped. Propagation uses them to detect
	// that another writer moved the object between enqueue and the write, and a
	// client that silently discarded them would make that detection unreachable
	// against a real provider while passing against anything that ignores them.
	if opts.IfMatch != "" {
		input.IfMatch = aws.String(opts.IfMatch)
	}
	if opts.IfNoneMatch != "" {
		input.IfNoneMatch = aws.String(opts.IfNoneMatch)
	}
	input.Metadata, err = storage.NormalizeUserMetadata(opts.Metadata)
	if err != nil {
		return "", err
	}
	setUpstreamPutChecksum(input, opts)
	out, err := c.s3.PutObject(ctx, input)
	if err != nil {
		return "", mapUpstreamError(err)
	}
	return normalizeETag(aws.ToString(out.ETag)), nil
}

func setUpstreamPutChecksum(input *s3.PutObjectInput, opts storage.PutOptions) {
	if opts.ChecksumAlgorithm != "" {
		input.ChecksumAlgorithm = types.ChecksumAlgorithm(opts.ChecksumAlgorithm)
		if opts.ChecksumValue != "" {
			switch opts.ChecksumAlgorithm {
			case "CRC64NVME":
				input.ChecksumCRC64NVME = aws.String(opts.ChecksumValue)
			case "CRC32":
				input.ChecksumCRC32 = aws.String(opts.ChecksumValue)
			case "CRC32C":
				input.ChecksumCRC32C = aws.String(opts.ChecksumValue)
			case "SHA1":
				input.ChecksumSHA1 = aws.String(opts.ChecksumValue)
			case "SHA256":
				input.ChecksumSHA256 = aws.String(opts.ChecksumValue)
			}
		}
	}
}

func (c *S3Client) DeleteObject(ctx context.Context, bucket, key, ifMatch string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:  aws.String(bucket),
		Key:     aws.String(key),
		IfMatch: aws.String(ifMatch),
	})
	return mapUpstreamError(err)
}

func (c *S3Client) ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	}
	if opts.Prefix != "" {
		input.Prefix = aws.String(opts.Prefix)
	}
	if opts.Delimiter != "" {
		input.Delimiter = aws.String(opts.Delimiter)
	}
	if opts.MaxKeys > 0 {
		input.MaxKeys = aws.Int32(int32(opts.MaxKeys))
	}
	if opts.ContinuationToken != "" {
		input.ContinuationToken = aws.String(opts.ContinuationToken)
	}
	if opts.StartAfter != "" {
		input.StartAfter = aws.String(opts.StartAfter)
	}

	out, err := c.s3.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, mapUpstreamError(err)
	}

	result := &storage.ListResult{
		IsTruncated:           aws.ToBool(out.IsTruncated),
		ContinuationToken:     aws.ToString(out.ContinuationToken),
		NextContinuationToken: aws.ToString(out.NextContinuationToken),
	}
	for _, obj := range out.Contents {
		result.Objects = append(result.Objects, objectToMeta(bucket, obj))
	}
	for _, cp := range out.CommonPrefixes {
		if p := aws.ToString(cp.Prefix); p != "" {
			result.CommonPrefixes = append(result.CommonPrefixes, p)
		}
	}
	result.KeyCount = len(result.Objects) + len(result.CommonPrefixes)
	return result, nil
}

func mapUpstreamError(err error) error {
	if err == nil {
		return nil
	}
	if upstreamHTTPStatus(err) == 412 {
		return storage.ErrPreconditionFailed
	}
	var noKey *types.NoSuchKey
	if errors.As(err, &noKey) {
		return storage.ErrObjectNotFound
	}
	var noBucket *types.NoSuchBucket
	if errors.As(err, &noBucket) {
		return storage.ErrBucketNotFound
	}
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) && respErr != nil && respErr.Response != nil && respErr.HTTPStatusCode() == 404 {
		return storage.ErrObjectNotFound
	}
	class := classifyRetry(err)
	if class == RetryClassUnknown {
		return err
	}
	return &UpstreamError{
		Err:        err,
		StatusCode: upstreamHTTPStatus(err),
		Code:       upstreamErrorCode(err),
		Class:      class,
	}
}

func headOutputToMeta(bucket, key string, out *s3.HeadObjectOutput) (*storage.ObjectMeta, error) {
	metadata, err := storage.NormalizeUserMetadata(out.Metadata)
	if err != nil {
		return nil, err
	}
	meta := &storage.ObjectMeta{
		Bucket:      bucket,
		Key:         key,
		Size:        aws.ToInt64(out.ContentLength),
		ETag:        normalizeETag(aws.ToString(out.ETag)),
		ContentType: aws.ToString(out.ContentType),
		Metadata:    metadata,
	}
	if out.ChecksumType != types.ChecksumTypeComposite {
		applyUpstreamChecksum(meta, []upstreamChecksum{
			{"CRC64NVME", out.ChecksumCRC64NVME}, {"CRC32", out.ChecksumCRC32},
			{"CRC32C", out.ChecksumCRC32C}, {"SHA1", out.ChecksumSHA1}, {"SHA256", out.ChecksumSHA256},
		})
	}
	if out.LastModified != nil {
		meta.LastModified = out.LastModified.UTC()
	}
	return meta, nil
}

func getOutputToMeta(bucket, key string, out *s3.GetObjectOutput) (*storage.ObjectMeta, error) {
	metadata, err := storage.NormalizeUserMetadata(out.Metadata)
	if err != nil {
		return nil, err
	}
	meta := &storage.ObjectMeta{
		Bucket:      bucket,
		Key:         key,
		Size:        aws.ToInt64(out.ContentLength),
		ETag:        normalizeETag(aws.ToString(out.ETag)),
		ContentType: aws.ToString(out.ContentType),
		Metadata:    metadata,
	}
	if out.ChecksumType != types.ChecksumTypeComposite {
		applyUpstreamChecksum(meta, []upstreamChecksum{
			{"CRC64NVME", out.ChecksumCRC64NVME}, {"CRC32", out.ChecksumCRC32},
			{"CRC32C", out.ChecksumCRC32C}, {"SHA1", out.ChecksumSHA1}, {"SHA256", out.ChecksumSHA256},
		})
	}
	if out.LastModified != nil {
		meta.LastModified = out.LastModified.UTC()
	}
	return meta, nil
}

func objectToMeta(bucket string, obj types.Object) storage.ObjectMeta {
	meta := storage.ObjectMeta{
		Bucket: bucket,
		Key:    aws.ToString(obj.Key),
		Size:   aws.ToInt64(obj.Size),
		ETag:   normalizeETag(aws.ToString(obj.ETag)),
	}
	if obj.LastModified != nil {
		meta.LastModified = obj.LastModified.UTC()
	}
	return meta
}

func normalizeETag(etag string) string {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return etag
	}
	if strings.HasPrefix(etag, "\"") && strings.HasSuffix(etag, "\"") {
		return etag
	}
	return "\"" + strings.Trim(etag, "\"") + "\""
}

type upstreamChecksum struct {
	algorithm string
	value     *string
}

func applyUpstreamChecksum(meta *storage.ObjectMeta, candidates []upstreamChecksum) {
	for _, candidate := range candidates {
		if candidate.value != nil && aws.ToString(candidate.value) != "" {
			meta.ChecksumAlgorithm = candidate.algorithm
			meta.ChecksumValue = aws.ToString(candidate.value)
			return
		}
	}
}

// upstreamChanged reports whether the upstream validator differs from the cached
// validator. ETag is a change detector here, not an ordering primitive.
func upstreamChanged(upstream, local *storage.ObjectMeta) bool {
	if upstream == nil {
		return false
	}
	if local == nil {
		return true
	}
	upETag := normalizeETag(upstream.ETag)
	localETag := normalizeETag(local.ETag)
	if upETag != "" && localETag != "" && upETag != localETag {
		return true
	}
	if !upstream.LastModified.IsZero() && !local.LastModified.IsZero() {
		return upstream.LastModified.After(local.LastModified)
	}
	return false
}
