package s3api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type copyZeroReader struct{}

type copySizedReader struct {
	io.Reader
	size int64
}

func (r copySizedReader) ReadSize() int64 { return r.size }

func (copyZeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func BenchmarkCopyReplace(b *testing.B) {
	for _, backend := range []string{"filesystem", "memory"} {
		for _, size := range []int64{1 << 20, 16 << 20, 64 << 20} {
			b.Run(fmt.Sprintf("%s/%dMiB", backend, size>>20), func(b *testing.B) {
				var store storage.Store = storage.NewMemoryStore()
				if backend == "filesystem" {
					filesystem, err := fs.NewFilesystemStore(b.TempDir())
					if err != nil {
						b.Fatal(err)
					}
					store = filesystem
				}
				instance, err := stowruntime.OpenWithStore(stowruntime.Options{MaxBytes: 512 << 20, MaxObjects: 16}, store, nil)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = instance.Close() })
				ctx := context.Background()
				if err := instance.CreateBucket(ctx, "bucket"); err != nil {
					b.Fatal(err)
				}
				adapter, err := stowruntime.NewStoreAdapter(instance)
				if err != nil {
					b.Fatal(err)
				}
				var source io.Reader = copySizedReader{Reader: io.LimitReader(copyZeroReader{}, size), size: size}
				if backend == "memory" {
					source = storage.NewOwnedBody(make([]byte, size))
				}
				if _, err := adapter.PutObject(ctx, "bucket", "source", source, storage.PutOptions{}); err != nil {
					b.Fatal(err)
				}
				server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
				if err != nil {
					b.Fatal(err)
				}
				request := httptest.NewRequest(http.MethodPut, "/bucket/target", nil)
				request.Header.Set("X-Amz-Copy-Source", "/bucket/source")
				request.Header.Set("X-Amz-Metadata-Directive", "REPLACE")
				b.SetBytes(size)
				b.ReportAllocs()
				runtime.GC()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					response := httptest.NewRecorder()
					server.ServeHTTP(response, request)
					if response.Code != http.StatusOK {
						b.Fatalf("REPLACE: %d %s", response.Code, response.Body.String())
					}
				}
			})
		}
	}
}
