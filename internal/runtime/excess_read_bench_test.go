package runtime

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func BenchmarkExcessRead(b *testing.B) {
	for _, size := range []int{3, 1 << 20} {
		for _, mode := range []string{"stream", "get", "guarded"} {
			b.Run(fmt.Sprintf("size=%d/mode=%s", size, mode), func(b *testing.B) { benchmarkExcessRead(b, size, mode) })
		}
	}
}

func benchmarkExcessRead(b *testing.B, size int, mode string) {
	instance, store := prepareExcessBenchmark(b, size)
	declared := int64(2)
	if size > 3 {
		declared = 3
	}
	instance.store = &readSizeHintStore{saveBodyStore: store, size: declared}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		object, _, err := observeExcessRead(instance, mode)
		if err == nil {
			if len(object.Data) != size {
				b.Fatalf("old success bytes=%d want=%d", len(object.Data), size)
			}
		} else if mode == "stream" {
			if len(object.Data) != int(declared) {
				b.Fatalf("failure prefix=%d want=%d", len(object.Data), declared)
			}
		} else if len(object.Data) != 0 {
			b.Fatalf("failed buffered read returned %d bytes", len(object.Data))
		}
	}
	b.StopTimer()
	if store.gets != b.N {
		b.Fatalf("opens=%d want=%d", store.gets, b.N)
	}
}

func prepareExcessBenchmark(b *testing.B, size int) (*Instance, *saveBodyStore) {
	b.Helper()
	store := &saveBodyStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 2 << 20, MaxObjects: 1}, store, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = instance.Close() })
	ctx := context.Background()
	if err := instance.CreateBucket(ctx, "probe"); err != nil {
		b.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "probe", "target", bytes.Repeat([]byte("x"), size), PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}}); err != nil {
		b.Fatal(err)
	}
	return instance, store
}
