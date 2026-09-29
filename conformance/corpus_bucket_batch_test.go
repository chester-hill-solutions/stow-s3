package conformance_test

import (
	"slices"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func runCorpusBucketLifecycle(c *sharedCorpusContext) {
	bucket := aws.String(c.bucket(c.testCase.Bucket))
	listed, err := c.env.Client.ListBuckets(c.ctx, &s3.ListBucketsInput{})
	if err != nil {
		c.t.Fatal(err)
	}
	found := false
	for _, item := range listed.Buckets {
		if aws.ToString(item.Name) == *bucket {
			found = true
		}
	}
	if !found {
		c.t.Fatal("created bucket absent from ListBuckets")
	}
	if _, err := c.env.Client.HeadBucket(c.ctx, &s3.HeadBucketInput{Bucket: bucket}); err != nil {
		c.t.Fatal(err)
	}
	_, err = c.env.Client.DeleteBucket(c.ctx, &s3.DeleteBucketInput{Bucket: bucket})
	if c.testCase.Expect.Status >= 400 {
		assertCorpusError(c.t, err, c.testCase.Expect)
		return
	}
	if err != nil {
		c.t.Fatal(err)
	}
	assertCorpusStatus(c.t, c.env, c.testCase.Expect.Status)
	_, err = c.env.Client.HeadBucket(c.ctx, &s3.HeadBucketInput{Bucket: bucket})
	assertCorpusError(c.t, err, sharedCorpusExpectation{Status: 404})
	if _, err := c.env.Client.CreateBucket(c.ctx, &s3.CreateBucketInput{Bucket: bucket}); err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.env.Client.HeadBucket(c.ctx, &s3.HeadBucketInput{Bucket: bucket}); err != nil {
		c.t.Fatal(err)
	}
}

func runCorpusBatchDelete(c *sharedCorpusContext) {
	keys := make([]types.ObjectIdentifier, 0, len(c.testCase.DeleteKeys))
	for _, key := range c.testCase.DeleteKeys {
		keys = append(keys, types.ObjectIdentifier{Key: aws.String(key)})
	}
	output, err := c.env.Client.DeleteObjects(c.ctx, &s3.DeleteObjectsInput{Bucket: aws.String(c.bucket(c.testCase.Bucket)), Delete: &types.Delete{Objects: keys, Quiet: aws.Bool(c.testCase.Quiet)}})
	if err != nil {
		c.t.Fatal(err)
	}
	assertCorpusStatus(c.t, c.env, c.testCase.Expect.Status)
	if len(output.Errors) != 0 {
		c.t.Fatalf("delete errors=%v", output.Errors)
	}
	deleted := make([]string, 0, len(output.Deleted))
	for _, item := range output.Deleted {
		deleted = append(deleted, aws.ToString(item.Key))
	}
	sort.Strings(deleted)
	if !slices.Equal(deleted, c.testCase.Expect.Deleted) {
		c.t.Fatalf("deleted=%v want %v", deleted, c.testCase.Expect.Deleted)
	}
	for _, key := range c.testCase.DeleteKeys {
		_, err := c.env.Client.HeadObject(c.ctx, &s3.HeadObjectInput{Bucket: aws.String(c.bucket(c.testCase.Bucket)), Key: aws.String(key)})
		assertCorpusError(c.t, err, sharedCorpusExpectation{Status: 404})
	}
}
