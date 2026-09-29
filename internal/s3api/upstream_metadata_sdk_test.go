package s3api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestSDKUpstreamMetadataCacheOffline(t *testing.T) {
	requests := make(chan http.Header, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			requests <- r.Header.Clone()
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", `"object"`)
			return
		}
		w.Header()["x-amz-meta-project"] = []string{"stow"}
		w.Header().Set("x-amz-checksum-crc64nvme", "jSnVw/bqjr4=")
		w.Header().Set("ETag", `"object"`)
		w.Header().Set("Content-Length", "11")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "hello world")
		}
	}))
	defer upstream.Close()
	origin, err := runthrough.NewS3Client(runthrough.UpstreamConfig{Endpoint: upstream.URL, AccessKey: "test", SecretKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := origin.PutObject(ctx, "metadata-bucket", "source", strings.NewReader("hello world"), storage.PutOptions{
		Metadata: map[string]string{"X-Amz-Meta-Project": "stow"}, ChecksumAlgorithm: "CRC64NVME", ChecksumValue: "jSnVw/bqjr4=",
	}); err != nil {
		t.Fatal(err)
	}
	headers := <-requests
	if headers.Get("x-amz-meta-project") != "stow" || headers.Get("x-amz-meta-x-amz-meta-project") != "" || headers.Get("x-amz-checksum-crc64nvme") != "jSnVw/bqjr4=" {
		t.Fatalf("upstream headers = %v", headers)
	}
	if _, err := origin.PutObject(ctx, "metadata-bucket", "conflict", strings.NewReader("hello world"), storage.PutOptions{Metadata: map[string]string{"X-Amz-Meta-Project": "one", "project": "two"}}); err == nil {
		t.Fatal("upstream accepted conflicting metadata")
	}
	local, cache := storage.NewMemoryStore(), storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "metadata-bucket"); err != nil {
		t.Fatal(err)
	}
	if err := cache.CreateBucket(ctx, "metadata-bucket"); err != nil {
		t.Fatal(err)
	}
	online := runthrough.NewWithCache(runthrough.Config{Policy: runthrough.PolicyReadThroughCache}, local, cache, origin)
	client := metadataSDK(t, online)
	assertSDKObject(t, client, "source", "stow")
	cached, err := cache.HeadObject(ctx, "metadata-bucket", "source")
	if err != nil || !reflect.DeepEqual(cached.Metadata, map[string]string{"project": "stow"}) {
		t.Fatalf("cache metadata = %v, %v", cached, err)
	}
	upstream.Close()
	offline := runthrough.NewWithCache(runthrough.Config{Offline: true}, local, cache, origin)
	client = metadataSDK(t, offline)
	assertSDKObject(t, client, "source", "stow")
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("metadata-bucket"), Key: aws.String("source")})
	if err != nil || aws.ToString(head.ChecksumCRC64NVME) != "jSnVw/bqjr4=" {
		t.Fatalf("offline head = %v, %v", head, err)
	}
}
