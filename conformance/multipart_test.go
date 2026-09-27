package conformance_test

// The multipart surface against the real AWS SDK: initiation properties reaching
// the completed object, and the minimum part size S3 enforces at completion.

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

func TestListMultipartUploads(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "uploads")
	createBucket(ctx, t, env.Client, bucket)

	created, err := env.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("large/object.bin"),
	})
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	uploadID := aws.ToString(created.UploadId)
	if uploadID == "" {
		t.Fatal("expected upload ID")
	}
	t.Cleanup(func() {
		_, _ = env.Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(bucket),
			Key:      aws.String("large/object.bin"),
			UploadId: aws.String(uploadID),
		})
	})

	out, err := env.Client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Fatalf("ListMultipartUploads: %v", err)
	}
	if len(out.Uploads) != 1 {
		t.Fatalf("expected one in-progress upload, got %d", len(out.Uploads))
	}
	if got := aws.ToString(out.Uploads[0].Key); got != "large/object.bin" {
		t.Fatalf("upload key = %q, want %q", got, "large/object.bin")
	}
	if got := aws.ToString(out.Uploads[0].UploadId); got != uploadID {
		t.Fatalf("upload ID = %q, want %q", got, uploadID)
	}
}

func TestMultipartUpload(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t)
	key := "large.bin"
	createBucket(ctx, t, env.Client, bucket)

	createOut, err := env.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	uploadID := aws.ToString(createOut.UploadId)
	if uploadID == "" {
		t.Fatal("empty upload id")
	}

	const minPart = 5 * 1024 * 1024
	part1 := bytes.Repeat([]byte("A"), minPart)
	part2 := []byte("final-part")

	etags := uploadNumberedParts(t, ctx, env, uploadTarget{bucket, key, uploadID}, part1, part2)

	listOut, err := env.Client.ListParts(ctx, &s3.ListPartsInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		t.Fatalf("ListParts: %v", err)
	}
	if len(listOut.Parts) != 2 {
		t.Fatalf("ListParts count = %d, want 2", len(listOut.Parts))
	}

	completeOut, err := env.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: []types.CompletedPart{
				{ETag: aws.String(etags[0]), PartNumber: aws.Int32(1)},
				{ETag: aws.String(etags[1]), PartNumber: aws.Int32(2)},
			},
		},
	})
	if err != nil {
		t.Fatalf("CompleteMultipartUpload: %v", err)
	}
	assertCompositeETag(t, completeOut.ETag, part1, part2)

	data := readCompletedObject(t, ctx, env, bucket, key)
	expectedLen := minPart + len(part2)
	if len(data) != expectedLen {
		t.Fatalf("object size = %d, want %d", len(data), expectedLen)
	}
	if !bytes.Equal(data[:minPart], part1) || !bytes.Equal(data[minPart:], part2) {
		t.Fatal("multipart object bytes mismatch")
	}
}

// uploadTarget names the upload the parts belong to: a bucket, a key and the
// upload ID, which is what a part request needs and what three separate strings
// invite to be transposed.
type uploadTarget struct {
	bucket   string
	key      string
	uploadID string
}

// uploadNumberedParts uploads one part per body, numbered from one, and returns
// their ETags in the same order.
func uploadNumberedParts(t *testing.T, ctx context.Context, env *testEnv, target uploadTarget, bodies ...[]byte) []string {
	t.Helper()
	etags := make([]string, 0, len(bodies))
	for index, body := range bodies {
		part, err := env.Client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(target.bucket),
			Key:        aws.String(target.key),
			UploadId:   aws.String(target.uploadID),
			PartNumber: aws.Int32(int32(index + 1)),
			Body:       bytes.NewReader(body),
		})
		if err != nil {
			t.Fatalf("UploadPart %d: %v", index+1, err)
		}
		etags = append(etags, aws.ToString(part.ETag))
	}
	return etags
}

// assertCompositeETag checks the ETag a genuine multipart completion returns: the
// digest of the concatenated part digests, with the part count in the suffix.
func assertCompositeETag(t *testing.T, got *string, bodies ...[]byte) {
	t.Helper()
	if got == nil || *got == "" {
		t.Fatal("expected a composite ETag")
	}
	digests := make([]byte, 0, len(bodies)*md5.Size)
	for _, body := range bodies {
		digest := md5.Sum(body)
		digests = append(digests, digest[:]...)
	}
	combined := md5.Sum(digests)
	want := fmt.Sprintf("\"%s-%d\"", hex.EncodeToString(combined[:]), len(bodies))
	if *got != want {
		t.Fatalf("composite ETag = %q, want %q", *got, want)
	}
}

func readCompletedObject(t *testing.T, ctx context.Context, env *testEnv, bucket, key string) []byte {
	t.Helper()
	get, err := env.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	defer get.Body.Close()
	data, err := io.ReadAll(get.Body)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	return data
}

func TestCompleteMultipartRejectsWrongETag(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "wrong-etag")
	createBucket(ctx, t, env.Client, bucket)
	created, err := env.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("object"),
	})
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	uploadID := aws.ToString(created.UploadId)
	part, err := env.Client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String("object"),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(1),
		Body:       strings.NewReader("body"),
	})
	if err != nil {
		t.Fatalf("UploadPart: %v", err)
	}
	_, err = env.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String("object"),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: []types.CompletedPart{{ETag: aws.String("\"wrong\""), PartNumber: aws.Int32(1)}},
		},
	})
	if err == nil {
		t.Fatal("expected wrong ETag completion to fail")
	}
	var responseErr *smithyhttp.ResponseError
	if !errors.As(err, &responseErr) || responseErr.HTTPStatusCode() != http.StatusBadRequest {
		t.Fatalf("expected 400, got %v", err)
	}
	_, _ = env.Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("object"), UploadId: aws.String(uploadID),
	})
	_ = part
}

// A completed multipart object carries the content type and user metadata its
// initiation named.
//
// The exact case the S3 surface has to get right, end to end through the real
// SDK: initiate a JSON upload with a metadata header, upload a part, complete, and
// read the object back. Before the storage model grew the initiation's object
// properties there was nowhere for them to live, so the completed object came
// back as application/octet-stream with no metadata — and a client that had
// asked for both had no way to tell it had been given something different from
// what it asked for.
func TestCompletedMultipartObjectCarriesInitiationProperties(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "multipart-properties")
	key := "document.json"
	createBucket(ctx, t, env.Client, bucket)

	created, err := env.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String("application/json"),
		Metadata:    map[string]string{"foo": "bar"},
	})
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	uploadID := aws.ToString(created.UploadId)

	body := []byte(`{"task":"summarize"}`)
	etags := uploadNumberedParts(t, ctx, env, uploadTarget{bucket, key, uploadID}, body)
	if _, err := env.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: []types.CompletedPart{{ETag: aws.String(etags[0]), PartNumber: aws.Int32(1)}},
		},
	}); err != nil {
		t.Fatalf("CompleteMultipartUpload: %v", err)
	}

	head, err := env.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	if got := aws.ToString(head.ContentType); got != "application/json" {
		t.Errorf("completed ContentType = %q, want application/json", got)
	}
	if got := head.Metadata["foo"]; got != "bar" {
		t.Errorf("completed metadata foo = %q, want bar", got)
	}

	// And the body, because the properties arriving must not have cost the bytes.
	if got := readCompletedObject(t, ctx, env, bucket, key); !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

// The minimum part size is S3's, and the three sizes it is stated in.
//
// One byte plus one byte is refused because the first part is not final. Five
// mebibytes plus one byte is accepted because the first part is exactly the
// minimum — the boundary belongs to the legal side, and an implementation that
// compares with the wrong operator refuses every legal multipart upload. One
// part of one byte is accepted because a single part is always final.
func TestCompleteMultipartMinimumPartSize(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "multipart-min-size")
	createBucket(ctx, t, env.Client, bucket)

	const minPart = 5 * 1024 * 1024
	cases := []struct {
		name       string
		sizes      []int
		wantReject bool
	}{
		{"one byte plus one byte", []int{1, 1}, true},
		{"five mebibytes plus one byte", []int{minPart, 1}, false},
		{"a single one-byte part", []int{1}, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := fmt.Sprintf("object-%d.bin", i)
			created, err := env.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(key),
			})
			if err != nil {
				t.Fatalf("CreateMultipartUpload: %v", err)
			}
			uploadID := aws.ToString(created.UploadId)
			t.Cleanup(func() {
				_, _ = env.Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
					Bucket: aws.String(bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
				})
			})

			// Distinct filler per part, so each ETag identifies its own part.
			bodies := make([][]byte, 0, len(tc.sizes))
			for index, size := range tc.sizes {
				bodies = append(bodies, bytes.Repeat([]byte{byte('a' + index)}, size))
			}
			etags := uploadNumberedParts(t, ctx, env, uploadTarget{bucket, key, uploadID}, bodies...)
			completed := make([]types.CompletedPart, 0, len(etags))
			for index := range etags {
				completed = append(completed, types.CompletedPart{
					ETag: aws.String(etags[index]), PartNumber: aws.Int32(int32(index + 1)),
				})
			}

			_, err = env.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
				Bucket:          aws.String(bucket),
				Key:             aws.String(key),
				UploadId:        aws.String(uploadID),
				MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
			})
			if tc.wantReject {
				var responseErr *smithyhttp.ResponseError
				if !errors.As(err, &responseErr) || responseErr.HTTPStatusCode() != http.StatusBadRequest {
					t.Fatalf("complete = %v, want 400", err)
				}
				if !strings.Contains(err.Error(), "EntityTooSmall") {
					t.Fatalf("complete error = %v, want EntityTooSmall", err)
				}
				// Nothing published, so the upload is still completable and the
				// object does not exist.
				if _, headErr := env.Client.HeadObject(ctx, &s3.HeadObjectInput{
					Bucket: aws.String(bucket), Key: aws.String(key),
				}); headErr == nil {
					t.Fatal("a refused completion published an object")
				}
				return
			}
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			head, err := env.Client.HeadObject(ctx, &s3.HeadObjectInput{
				Bucket: aws.String(bucket), Key: aws.String(key),
			})
			if err != nil {
				t.Fatalf("HeadObject: %v", err)
			}
			var want int64
			for _, size := range tc.sizes {
				want += int64(size)
			}
			if aws.ToInt64(head.ContentLength) != want {
				t.Fatalf("completed size = %d, want %d", aws.ToInt64(head.ContentLength), want)
			}
		})
	}
}

// Naming a checksum algorithm at initiation must not fail the completion.
//
// x-amz-checksum-algorithm on CreateMultipartUpload says which algorithm the
// client means to use. The initiation has no body, so no value comes with it -
// the record is a declaration, not a claim about the object.
//
// It was handed to the store as a claim, and the store rightly refuses a claim
// with no value, so every completion of an upload that named one answered
// 500 InternalError. The SDK retried three times before giving up, and the
// client's own error said nothing about the cause. This is the case at the wire
// and through the real SDK, because that is where it presented as a service
// outage rather than as a bug in a store.
func TestMultipartCompletionSucceedsWhenInitiationNamesAChecksumAlgorithm(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "multipart-checksum-declaration")
	key := "payload.bin"
	createBucket(ctx, t, env.Client, bucket)

	created, err := env.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(key),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32,
	})
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	uploadID := aws.ToString(created.UploadId)

	body := []byte("hello")
	etags := uploadNumberedParts(t, ctx, env, uploadTarget{bucket, key, uploadID}, body)
	if _, err := env.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: []types.CompletedPart{{ETag: aws.String(etags[0]), PartNumber: aws.Int32(1)}},
		},
	}); err != nil {
		t.Fatalf("CompleteMultipartUpload after naming a checksum algorithm: %v", err)
	}

	if got := readCompletedObject(t, ctx, env, bucket, key); !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}
