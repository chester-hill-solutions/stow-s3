package stow_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func TestWorkspaceFacadeRejectsStaleMultipartWriteAfterHostEdit(t *testing.T) {
	ws := openWorkspace(t, t.TempDir())
	facade, err := ws.Facade()
	if err != nil {
		t.Fatal(err)
	}
	client, ctx := facadeS3(facade), context.Background()
	bucket, key := aws.String(ws.Bucket()), aws.String("artifact")
	upload, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: bucket, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	part, err := client.UploadPart(ctx, &s3.UploadPartInput{Bucket: bucket, Key: key, UploadId: upload.UploadId, PartNumber: aws.Int32(1), Body: strings.NewReader("old")})
	if err != nil {
		t.Fatal(err)
	}
	complete, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: bucket, Key: key, UploadId: upload.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{PartNumber: aws.Int32(1), ETag: part.ETag}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws.Dir(), "artifact")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: bucket, Key: key, Body: strings.NewReader("overwrite"), IfMatch: complete.ETag})
	var apiError smithy.APIError
	if !errors.As(err, &apiError) || apiError.ErrorCode() != "PreconditionFailed" {
		t.Fatalf("stale S3 write was accepted: %v", err)
	}
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: bucket, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	data, err := io.ReadAll(object.Body)
	if err != nil || string(data) != "new" || aws.ToString(object.ETag) == aws.ToString(complete.ETag) {
		t.Fatalf("host edit was not preserved: %q %v", data, err)
	}
}
