package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func BenchmarkReadForSaveObservation(b *testing.B) {
	for _, item := range []struct {
		name   string
		err    error
		exists bool
	}{
		{"present", nil, true},
		{"missing", nil, false},
		{"wrapped-missing", fmt.Errorf("open: %w", storage.ErrObjectNotFound), false},
		{"unknown", errors.New("read unavailable"), true},
		{"marker-first", objectHeadAmbiguity("marker-first"), true},
		{"missing-first", objectHeadAmbiguity("missing-first"), true},
	} {
		b.Run(item.name, func(b *testing.B) {
			ctx := context.Background()
			store := &saveReadFaultStore{MemoryStore: storage.NewMemoryStore()}
			instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 8, MaxObjects: 1}, store, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = instance.Close() })
			if err := instance.CreateBucket(ctx, "probe"); err != nil {
				b.Fatal(err)
			}
			if item.exists {
				if _, err := instance.PutObject(ctx, "probe", "target", []byte("old"), PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}}); err != nil {
					b.Fatal(err)
				}
			}
			store.readErr = item.err
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				object, _, err := instance.ReadForSave(ctx, "probe", "target")
				if err != nil && err != item.err {
					b.Fatal(err)
				}
				if object.Size != 0 && object.Size != 3 {
					b.Fatal(object.Size)
				}
			}
			b.StopTimer()
			if store.gets != b.N {
				b.Fatalf("lookups=%d want=%d", store.gets, b.N)
			}
		})
	}
}
