package runtime

import (
	"bytes"
	"context"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func BenchmarkOpenedObjectRead(b *testing.B) {
	for _, item := range []struct {
		name             string
		size             int
		guarded, failure bool
	}{
		{"get-tiny", 3, false, false},
		{"get-64KiB", 64 << 10, false, false},
		{"save-64KiB", 64 << 10, true, false},
		{"get-body-missing", 3, false, true},
		{"save-body-missing", 3, true, true},
	} {
		b.Run(item.name, func(b *testing.B) { benchmarkOpenedRead(b, item.size, item.guarded, item.failure) })
	}
}

func benchmarkOpenedRead(b *testing.B, size int, guarded, failure bool) {
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
	if _, err := instance.PutObject(ctx, "probe", "target", bytes.Repeat([]byte("x"), size), PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}}); err != nil {
		b.Fatal(err)
	}
	if failure {
		store.fault = &saveBodyFault{after: 1, err: storage.ErrObjectNotFound}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var object Object
		var err error
		if guarded {
			object, _, err = instance.ReadForSave(ctx, "probe", "target")
		} else {
			object, err = instance.GetObject(ctx, "probe", "target")
		}
		if failure {
			if err != nil && err != storage.ErrObjectNotFound {
				b.Fatal(err)
			}
		} else if err != nil || len(object.Data) != size {
			b.Fatalf("length=%d error=%v", len(object.Data), err)
		}
	}
	b.StopTimer()
	if store.gets != b.N {
		b.Fatalf("GET calls=%d want=%d", store.gets, b.N)
	}
	if failure {
		assertSaveBodyCounts(b, store, 1, b.N)
	}
}
