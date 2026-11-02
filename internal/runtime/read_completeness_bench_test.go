package runtime

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func BenchmarkBufferedEOFRead(b *testing.B) {
	for _, guarded := range []bool{false, true} {
		for _, after := range []int{0, 1, 3} {
			b.Run(fmt.Sprintf("guarded=%t/after=%d", guarded, after), func(b *testing.B) { benchmarkBufferedEOF(b, guarded, after) })
		}
	}
}

func benchmarkBufferedEOF(b *testing.B, guarded bool, after int) {
	ctx := context.Background()
	store := &saveBodyStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 1 << 20, MaxObjects: 1}, store, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "probe"); err != nil {
		b.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "probe", "target", []byte("old"), PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}}); err != nil {
		b.Fatal(err)
	}
	store.fault = &saveBodyFault{after: after, together: true, err: io.EOF}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		object, _, err := readCompletionObservation(instance, guarded)
		if after == 3 {
			if err != nil || len(object.Data) != 3 {
				b.Fatalf("complete EOF bytes=%d error=%v", len(object.Data), err)
			}
		} else if err != nil && err != io.ErrUnexpectedEOF {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	assertSaveBodyCounts(b, store, after, b.N)
}
