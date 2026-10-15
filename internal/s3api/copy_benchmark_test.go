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
	benchmarkCopyOperation(b, "REPLACE", false)
}

func BenchmarkConditionalCopy(b *testing.B) {
	for _, profile := range []string{"native", "fallback"} {
		b.Run(profile, func(b *testing.B) {
			benchmarkCopyOperation(b, "COPY", profile == "fallback")
		})
	}
}

func benchmarkCopyOperation(b *testing.B, directive string, fallback bool) {
	for _, backend := range []string{"filesystem", "memory"} {
		for _, size := range []int64{1 << 20, 16 << 20, 64 << 20} {
			b.Run(fmt.Sprintf("%s/%dMiB", backend, size>>20), func(b *testing.B) {
				server, source := copyBenchmarkServer(b, backend, size, fallback)
				request := httptest.NewRequest(http.MethodPut, "/bucket/target", nil)
				request.Header.Set("X-Amz-Copy-Source", "/bucket/source")
				request.Header.Set("X-Amz-Metadata-Directive", directive)
				if directive == "COPY" {
					request.Header.Set("X-Amz-Copy-Source-If-Match", source.ETag)
				}
				b.SetBytes(size)
				b.ReportAllocs()
				runtime.GC()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					response := httptest.NewRecorder()
					server.ServeHTTP(response, request)
					if response.Code != http.StatusOK {
						b.Fatalf("%s: %d %s", directive, response.Code, response.Body.String())
					}
				}
			})
		}
	}
}

func copyBenchmarkServer(b *testing.B, backend string, size int64, fallback bool) (*s3api.Server, *storage.ObjectMeta) {
	b.Helper()
	var store storage.Store = storage.NewMemoryStore()
	if backend == "filesystem" {
		filesystem, err := fs.NewFilesystemStore(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		store = filesystem
	}
	if fallback {
		store = struct{ storage.Store }{store}
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
	var body io.Reader = copySizedReader{Reader: io.LimitReader(copyZeroReader{}, size), size: size}
	if backend == "memory" {
		body = storage.NewOwnedBody(make([]byte, size))
	}
	source, err := adapter.PutObject(ctx, "bucket", "source", body, storage.PutOptions{})
	if err != nil {
		b.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		b.Fatal(err)
	}
	return server, source
}
