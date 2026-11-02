package workspace_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func BenchmarkWorkspaceMultipartComparison(b *testing.B) {
	if os.Getenv("RAILWAY_SERVICE_ID") != "83497842-c51d-420e-8ba1-181589730ad4" || os.Getenv("RAILWAY_DEPLOYMENT_ID") == "" {
		b.Fatal("multipart benchmarks run only on the Railway runner")
	}
	for _, size := range []struct{ count, bytes int }{{1, 1024}, {16, 1 << 20}} {
		for _, operation := range []string{"list", "replace", "runtime-replace"} {
			name := fmt.Sprintf("parts=%d/bytes=%d/%s", size.count, size.bytes, operation)
			b.Run(name, func(b *testing.B) { benchmarkMultipartOperation(b, size.count, size.bytes, operation) })
		}
	}
}

func multipartBenchmarkStore(b *testing.B, count int, data []byte) (*workspace.Store, string) {
	b.Helper()
	store, err := workspace.New(workspace.Options{Root: b.TempDir(), Bucket: "probe"})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	upload, err := store.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
	if err != nil {
		b.Fatal(err)
	}
	for n := 1; n <= count; n++ {
		if _, err := store.UploadPart(ctx, upload.UploadID, n, bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
	return store, upload.UploadID
}

func benchmarkMultipartOperation(b *testing.B, count, size int, operation string) {
	b.Helper()
	data := bytes.Repeat([]byte("p"), size)
	store, id := multipartBenchmarkStore(b, count, data)
	ctx := context.Background()
	var instance *stowruntime.Instance
	if operation == "runtime-replace" {
		var err error
		instance, err = stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendWorkspace, MaxBytes: 128 << 20, MaxObjects: 1}, store, nil)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = instance.Close() })
	}
	b.ReportAllocs()
	b.ResetTimer()
	switch operation {
	case "list":
		for n := 0; n < b.N; n++ {
			parts, err := store.ListParts(ctx, id)
			if err != nil || len(parts) != count {
				b.Fatalf("listing=%d error=%v", len(parts), err)
			}
		}
	case "replace":
		reader := bytes.NewReader(data)
		for n := 0; n < b.N; n++ {
			reader.Reset(data)
			if _, err := store.UploadPart(ctx, id, 1, reader); err != nil {
				b.Fatal(err)
			}
		}
	case "runtime-replace":
		reader := bytes.NewReader(data)
		for n := 0; n < b.N; n++ {
			reader.Reset(data)
			if _, err := instance.UploadPart(ctx, id, 1, reader); err != nil {
				b.Fatal(err)
			}
		}
	}
}
