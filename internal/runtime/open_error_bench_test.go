package runtime

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type openingBenchmarkStore struct {
	*storage.MemoryStore
	reader  io.ReadCloser
	failure error
}

func (s *openingBenchmarkStore) GetObject(context.Context, string, string) (io.ReadCloser, *storage.ObjectMeta, error) {
	return s.reader, nil, s.failure
}

type openingBenchmarkReader struct{ closes int }

func (r *openingBenchmarkReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *openingBenchmarkReader) Close() error             { r.closes++; return nil }

func BenchmarkOpeningError(b *testing.B) {
	for _, hasReader := range []bool{false, true} {
		name := "nil"
		if hasReader {
			name = "reader"
		}
		b.Run(name, func(b *testing.B) {
			reader := &openingBenchmarkReader{}
			store := &openingBenchmarkStore{MemoryStore: storage.NewMemoryStore(), failure: errors.New("opening refused")}
			if hasReader {
				store.reader = reader
			}
			instance, err := OpenWithStore(Options{Backend: BackendMemory}, store, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				result, _, err := instance.OpenObject(ctx, "probe", "target")
				if result != nil || err != store.failure {
					b.Fatal("opening error contract changed")
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(reader.closes)/float64(b.N), "close/op")
		})
	}
}
