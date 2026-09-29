package s3api_test

import (
	"context"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

func metadataSDK(t *testing.T, store storage.Store) *s3.Client {
	t.Helper()
	server, err := s3api.New(s3api.Config{Store: store, Auth: s3api.DevBypass, Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(server.Handler())
	t.Cleanup(endpoint.Close)
	return s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint.URL)
		o.UsePathStyle = true
	})
}

func TestSDKMetadataAndCRC64SurviveCopyMultipartAndReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	client := metadataSDK(t, store)
	bucket := aws.String("metadata-bucket")
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: bucket}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: bucket, Key: aws.String("source"), Body: strings.NewReader("hello world"), Metadata: map[string]string{"Project": "stow"}, ChecksumAlgorithm: types.ChecksumAlgorithmCrc64nvme}); err != nil {
		t.Fatal(err)
	}
	assertSDKObject(t, client, "source", "stow")
	if _, err := client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: bucket, Key: aws.String("copy"), CopySource: aws.String("metadata-bucket/source")}); err != nil {
		t.Fatal(err)
	}
	assertSDKObject(t, client, "copy", "stow")
	if _, err := client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: bucket, Key: aws.String("replace"), CopySource: aws.String("metadata-bucket/source"), MetadataDirective: types.MetadataDirectiveReplace, Metadata: map[string]string{"Project": "replacement"}}); err != nil {
		t.Fatal(err)
	}
	assertSDKObject(t, client, "replace", "replacement")
	upload, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: bucket, Key: aws.String("multipart"), Metadata: map[string]string{"Project": "multipart"}})
	if err != nil {
		t.Fatal(err)
	}
	part, err := client.UploadPart(ctx, &s3.UploadPartInput{Bucket: bucket, Key: aws.String("multipart"), UploadId: upload.UploadId, PartNumber: aws.Int32(1), Body: strings.NewReader("hello world")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: bucket, Key: aws.String("multipart"), UploadId: upload.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{PartNumber: aws.Int32(1), ETag: part.ETag}}}}); err != nil {
		t.Fatal(err)
	}
	assertSDKObject(t, client, "multipart", "multipart")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	client = metadataSDK(t, reopened)
	assertSDKObject(t, client, "source", "stow")
	assertSDKObject(t, client, "multipart", "multipart")
}

func assertSDKObject(t *testing.T, client *s3.Client, key, project string) {
	t.Helper()
	want := map[string]string{"project": project}
	ctx := context.Background()
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("metadata-bucket"), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(head.Metadata, want) {
		t.Fatalf("HEAD %s metadata = %v", key, head.Metadata)
	}
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("metadata-bucket"), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(object.Body)
	object.Body.Close()
	if err != nil || string(body) != "hello world" || !reflect.DeepEqual(object.Metadata, want) {
		t.Fatalf("GET %s: body %q metadata %v error %v", key, body, object.Metadata, err)
	}
	if key != "multipart" && aws.ToString(object.ChecksumCRC64NVME) != "jSnVw/bqjr4=" {
		t.Fatalf("GET %s checksum = %v", key, object.ChecksumCRC64NVME)
	}
	if key != "source" {
		return
	}
	partial, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("metadata-bucket"), Key: aws.String(key), Range: aws.String("bytes=0-4")})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(partial.Body)
	partial.Body.Close()
	if err != nil || string(data) != "hello" || partial.ChecksumCRC64NVME != nil {
		t.Fatalf("range: %q %v checksum %v", data, err, partial.ChecksumCRC64NVME)
	}
}

func TestSDKReadsLegacyAndBareMetadataAndRejectsConflict(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(ctx, "metadata-bucket"); err != nil {
		t.Fatal(err)
	}
	client := metadataSDK(t, store)
	for key, metadata := range map[string]map[string]string{
		"legacy":   {"X-Amz-Meta-Project": "stow"},
		"bare":     {"Project": "stow"},
		"conflict": {"X-Amz-Meta-Project": "stow", "project": "other"},
	} {
		if _, err := store.PutObject(ctx, "metadata-bucket", key, strings.NewReader("hello world"), storage.PutOptions{Metadata: metadata}); err != nil {
			t.Fatal(err)
		}
		head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("metadata-bucket"), Key: aws.String(key)})
		if key == "conflict" {
			if err == nil {
				t.Fatal("conflicting aliases were accepted")
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(head.Metadata, map[string]string{"project": "stow"}) {
			t.Fatalf("%s: %v, %v", key, head, err)
		}
	}
}
