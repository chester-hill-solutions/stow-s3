package s3api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type copyPathStore struct {
	storage.Store
	gets, puts, copies, conditionalCopies int
}

func (s *copyPathStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	s.gets++
	return s.Store.GetObject(ctx, bucket, key)
}

func (s *copyPathStore) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	s.puts++
	return s.Store.PutObject(ctx, bucket, key, body, opts)
}

func (s *copyPathStore) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (*storage.ObjectMeta, error) {
	s.copies++
	return s.Store.CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey)
}

type conditionalCopyPath struct{ *copyPathStore }

func (s conditionalCopyPath) CopyObjectCond(ctx context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	s.conditionalCopies++
	return s.Store.(storage.ConditionalCopyStore).CopyObjectCond(ctx, req)
}

func TestCopyKeepsBackendCopyWithoutPayloadIO(t *testing.T) {
	for _, profile := range []string{"direct", "run-through", "runtime"} {
		for _, capability := range []string{"basic", "conditional"} {
			for _, condition := range []string{"none", "etag", "date"} {
				t.Run(profile+"/"+capability+"/"+condition, func(t *testing.T) {
					checkCopyPayloadIO(t, profile, capability, condition)
				})
			}
		}
	}
}

func checkCopyPayloadIO(t *testing.T, profile, capability, condition string) {
	t.Helper()
	ctx := context.Background()
	memory := storage.NewMemoryStore()
	if err := memory.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	meta, err := memory.PutObject(ctx, "bucket", "source", strings.NewReader(strings.Repeat("s", 1<<20)), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	counted := &copyPathStore{Store: memory}
	var store storage.Store = counted
	if capability == "conditional" {
		store = conditionalCopyPath{counted}
	}
	server := copyCostServer(t, store, profile)
	request := httptest.NewRequest(http.MethodPut, "/bucket/target", nil)
	request.Header.Set("X-Amz-Copy-Source", "/bucket/source")
	switch condition {
	case "etag":
		request.Header.Set("X-Amz-Copy-Source-If-Match", meta.ETag)
	case "date":
		request.Header.Set("X-Amz-Copy-Source-If-Unmodified-Since", meta.LastModified.Add(time.Hour).Format(http.TimeFormat))
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("COPY status=%d: %s", response.Code, response.Body.String())
	}
	if counted.gets != 0 || counted.puts != 0 {
		t.Errorf("COPY transferred payload through GET=%d, PUT=%d; want neither", counted.gets, counted.puts)
	}
	if counted.copies+counted.conditionalCopies != 1 {
		t.Errorf("backend COPY calls=%d, conditional COPY calls=%d; want one copy", counted.copies, counted.conditionalCopies)
	}
}

func copyCostServer(t *testing.T, store storage.Store, profile string) *s3api.Server {
	t.Helper()
	if profile == "run-through" {
		adapter := runthrough.New(runthrough.Config{}, store, nil)
		t.Cleanup(func() { _ = adapter.Close() })
		store = adapter
	}
	if profile == "runtime" {
		instance, err := stowruntime.OpenWithStore(stowruntime.Options{MaxBytes: 4 << 20}, store, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = instance.Close() })
		store, err = stowruntime.NewStoreAdapter(instance)
		if err != nil {
			t.Fatal(err)
		}
	}
	server, err := s3api.New(s3api.Config{Store: store, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	return server
}
