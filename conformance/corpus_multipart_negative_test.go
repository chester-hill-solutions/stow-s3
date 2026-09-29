package conformance_test

import (
	"bytes"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func runCorpusMultipartFailure(c *sharedCorpusContext) {
	t := c.t
	ref := &s3.CreateMultipartUploadInput{Bucket: aws.String(c.bucket(c.testCase.Bucket)), Key: aws.String(c.testCase.Key)}
	created, err := c.env.Client.CreateMultipartUpload(c.ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	abort := &s3.AbortMultipartUploadInput{Bucket: ref.Bucket, Key: ref.Key, UploadId: created.UploadId}
	defer c.env.Client.AbortMultipartUpload(c.ctx, abort)
	parts := []types.CompletedPart{}
	for _, part := range c.testCase.Parts {
		uploaded, err := c.env.Client.UploadPart(c.ctx, &s3.UploadPartInput{Bucket: ref.Bucket, Key: ref.Key, UploadId: created.UploadId, PartNumber: aws.Int32(int32(part.Number)), Body: bytes.NewReader(corpusPartBody(part))})
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, types.CompletedPart{PartNumber: aws.Int32(int32(part.Number)), ETag: uploaded.ETag})
	}
	parts = corpusInvalidCompletion(parts, c.testCase.MultipartFailure)
	input := &s3.CompleteMultipartUploadInput{Bucket: ref.Bucket, Key: ref.Key, UploadId: created.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: parts}}
	switch c.testCase.MultipartFailure {
	case "aborted":
		if _, err := c.env.Client.AbortMultipartUpload(c.ctx, abort); err != nil {
			t.Fatal(err)
		}
	case "repeated":
		if _, err := c.env.Client.CompleteMultipartUpload(c.ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	_, err = c.env.Client.CompleteMultipartUpload(c.ctx, input)
	assertCorpusError(t, err, c.testCase.Expect)
}

func corpusInvalidCompletion(parts []types.CompletedPart, failure string) []types.CompletedPart {
	switch failure {
	case "empty":
		return nil
	case "duplicate":
		return append(parts, parts[0])
	case "unsorted":
		return []types.CompletedPart{parts[1], parts[0]}
	case "wrong-etag":
		parts[0].ETag = aws.String("\"wrong-etag\"")
	}
	return parts
}
