package conformance_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestMultipartCRC64NVMeFullObjectChecksum(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "multipart-checksum")
	createBucket(ctx, t, env.Client, bucket)

	payload := strings.Repeat("multipart checksum payload ", 64)
	want, err := storage.ComputeChecksum("CRC64NVME", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}

	upload := multipartWithFullObjectChecksum(t, env, bucket, payload)
	if upload.ChecksumCRC64NVME == nil || *upload.ChecksumCRC64NVME != want {
		t.Fatalf("complete checksum = %v, want %s", upload.ChecksumCRC64NVME, want)
	}
	head, err := env.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String("object")})
	if err != nil {
		t.Fatal(err)
	}
	if head.ChecksumCRC64NVME == nil || *head.ChecksumCRC64NVME != want {
		t.Fatalf("HEAD checksum = %v, want %s", head.ChecksumCRC64NVME, want)
	}

	mismatched := multipartUpload(t, env, bucket, payload, "AAAAAA==")
	if _, err := env.Client.CompleteMultipartUpload(ctx, completionInput(bucket, mismatched, "AAAAAA==")); err == nil {
		t.Fatal("a mismatched full-object checksum was accepted")
	}
}

type multipartHandles struct {
	uploadID string
	etag     *string
}

func multipartWithFullObjectChecksum(t *testing.T, env *testEnv, bucket, payload string) *s3.CompleteMultipartUploadOutput {
	t.Helper()
	want, err := storage.ComputeChecksum("CRC64NVME", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	handles := multipartUpload(t, env, bucket, payload, want)
	output, err := env.Client.CompleteMultipartUpload(context.Background(), completionInput(bucket, handles, want))
	if err != nil {
		t.Fatalf("complete with %s: %v", want, err)
	}
	return output
}

func multipartUpload(t *testing.T, env *testEnv, bucket, payload, checksum string) multipartHandles {
	t.Helper()
	ctx := context.Background()
	created, err := env.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("object"),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc64nvme, ChecksumType: types.ChecksumTypeFullObject,
	})
	if err != nil {
		t.Fatalf("create multipart: %v", err)
	}
	part, err := env.Client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(bucket), Key: aws.String("object"), UploadId: created.UploadId,
		PartNumber: aws.Int32(1), Body: strings.NewReader(payload),
	})
	if err != nil {
		t.Fatalf("upload part: %v", err)
	}
	return multipartHandles{uploadID: *created.UploadId, etag: part.ETag}
}

func completionInput(bucket string, handles multipartHandles, checksum string) *s3.CompleteMultipartUploadInput {
	return &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("object"), UploadId: aws.String(handles.uploadID),
		MultipartUpload:   &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{ETag: handles.etag, PartNumber: aws.Int32(1)}}},
		ChecksumCRC64NVME: aws.String(checksum), ChecksumType: types.ChecksumTypeFullObject,
	}
}
