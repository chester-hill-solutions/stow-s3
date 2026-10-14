package runtime

import (
	"bytes"
	"context"
	"testing"
)

func TestStreamPutAllocationBounded(t *testing.T) {
	instance, err := Open(Options{MaxBytes: 16 << 20, MaxObjects: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	ctx := context.Background()
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), 1<<20)
	result := testing.Benchmark(func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			_, err := instance.PutObjectStream(ctx, "bucket", "large", bytes.NewReader(data), StreamPutOptions{Size: int64(len(data))})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	if result.AllocedBytesPerOp() > 2<<20 {
		t.Fatalf("PUT repeatedly grows payload buffer: %d allocated bytes/op for a 1 MiB stream", result.AllocedBytesPerOp())
	}
}
