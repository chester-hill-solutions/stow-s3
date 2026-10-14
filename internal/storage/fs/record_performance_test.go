package fs

import (
	"bytes"
	"context"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestHeadAllocationBounded(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), 8<<20)
	if _, err := store.PutObject(ctx, "bucket", "large", bytes.NewReader(data), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	result := testing.Benchmark(func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			meta, err := store.HeadObject(ctx, "bucket", "large")
			if err != nil || meta.Size != int64(len(data)) {
				b.Fatalf("head=%+v, %v", meta, err)
			}
		}
	})
	if result.AllocedBytesPerOp() > 64<<10 {
		t.Fatalf("HEAD materializes payload: %d allocated bytes/op for an 8 MiB object", result.AllocedBytesPerOp())
	}
}
