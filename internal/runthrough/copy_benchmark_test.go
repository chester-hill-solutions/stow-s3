package runthrough_test

import (
	"context"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func BenchmarkCopyConditions(b *testing.B) {
	for _, profile := range []string{"basic", "conditional"} {
		for _, conditioned := range []bool{false, true} {
			name := profile + "/unconditional"
			if conditioned {
				name = profile + "/conditioned"
			}
			b.Run(name, func(b *testing.B) {
				ctx := context.Background()
				memory := storage.NewMemoryStore()
				if err := memory.CreateBucket(ctx, "bucket"); err != nil {
					b.Fatal(err)
				}
				source, err := memory.PutObject(ctx, "bucket", "source", strings.NewReader(strings.Repeat("s", 4096)), storage.PutOptions{})
				if err != nil {
					b.Fatal(err)
				}
				var local storage.Store = struct{ storage.Store }{memory}
				if profile == "conditional" {
					local = memory
				}
				adapter := runthrough.New(runthrough.Config{}, local, nil)
				b.Cleanup(func() { _ = adapter.Close() })
				request := storage.CopyRequest{SourceBucket: "bucket", SourceKey: "source", DestBucket: "bucket", DestKey: "target"}
				if conditioned {
					request.Options.SourceIfMatch = source.ETag
				}
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					if _, err := adapter.CopyObjectCond(ctx, request); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
