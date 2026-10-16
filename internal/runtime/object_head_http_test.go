package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type objectHeadHTTPStore struct {
	*storage.MemoryStore
	headErr error
}

func (s *objectHeadHTTPStore) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	if s.headErr != nil {
		return nil, s.headErr
	}
	return s.MemoryStore.HeadObject(ctx, bucket, key)
}

func TestObjectHeadS3DeletionRefusesAmbiguity(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, missingFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("batch=%t/missingFirst=%t", batch, missingFirst), func(t *testing.T) { checkObjectHeadHTTP(t, batch, missingFirst) })
		}
	}
}

func checkObjectHeadHTTP(t *testing.T, batch, missingFirst bool) {
	t.Helper()
	ctx := context.Background()
	store := &objectHeadHTTPStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendMemory, MaxBytes: 8, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	before, err := instance.PutObject(ctx, "probe", "target", []byte("old"), stowruntime.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	store.headErr = errors.Join(storage.ErrMutationCommitted, storage.ErrObjectNotFound)
	if missingFirst {
		store.headErr = errors.Join(storage.ErrObjectNotFound, storage.ErrMutationCommitted)
	}
	server := objectHeadHTTPServer(t, instance)
	method, url, body := http.MethodDelete, "/probe/target", ""
	if batch {
		method, url, body = http.MethodPost, "/probe?delete", "<Delete><Object><Key>target</Key></Object></Delete>"
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, objectHeadHTTPRequest(method, url, body))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "<Code>InternalError</Code>") {
		t.Errorf("uncertain HEAD: status=%d body=%s", response.Code, response.Body.String())
	}
	object, err := instance.GetObject(ctx, "probe", "target")
	if err != nil || string(object.Data) != "old" || object.ETag != before.ETag || object.VersionID != before.VersionID {
		t.Errorf("unknown HEAD allowed deletion: object=%+v error=%v", object, err)
	}
	store.headErr = nil
	response = httptest.NewRecorder()
	server.ServeHTTP(response, objectHeadHTTPRequest(method, url, body))
	expected := http.StatusNoContent
	if batch {
		expected = http.StatusOK
	}
	if response.Code != expected {
		t.Errorf("healthy retry: status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := instance.PutObject(ctx, "probe", "capacity", []byte("12345678"), stowruntime.PutOptions{}); err != nil {
		t.Errorf("capacity stranded: %v usage=%+v", err, instance.Usage())
	}
}

func objectHeadHTTPServer(t *testing.T, instance *stowruntime.Instance) *s3api.Server {
	t.Helper()
	adapter, err := stowruntime.NewStoreAdapter(instance)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func objectHeadHTTPRequest(method, url, body string) *http.Request {
	request := httptest.NewRequest(method, url, strings.NewReader(body))
	request.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return request
}
