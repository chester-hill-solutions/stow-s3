package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type streamBenchmarkCase struct {
	name                         string
	size, chunk                  int
	adapter, seek, ranged, short bool
}

func streamBenchmarkCases() []streamBenchmarkCase {
	return []streamBenchmarkCase{
		{name: "tiny", size: 3, chunk: 3},
		{name: "64KiB-32KiB", size: 64 << 10, chunk: 32 << 10},
		{name: "64KiB-1KiB", size: 64 << 10, chunk: 1 << 10},
		{name: "seek", size: 64 << 10, chunk: 32 << 10, seek: true},
		{name: "range", size: 64 << 10, chunk: 32 << 10, ranged: true},
		{name: "adapter", size: 3, chunk: 3, adapter: true},
		{name: "short", size: 3, chunk: 3, short: true},
	}
}

func BenchmarkStreamCompleteness(b *testing.B) {
	for _, item := range streamBenchmarkCases() {
		b.Run(item.name, func(b *testing.B) { benchmarkStreamCompleteness(b, item, nil) })
	}
}

type streamBenchmarkOpener func(*Instance, streamBenchmarkCase) (io.ReadCloser, error)

func openStreamBenchmark(instance *Instance, item streamBenchmarkCase) (io.ReadCloser, error) {
	ctx := context.Background()
	if item.ranged {
		reader, _, err := instance.OpenObjectRange(ctx, "probe", "target", storage.ObjectRange{Offset: 1, Length: int64(item.size - 1)})
		return reader, err
	}
	if item.adapter {
		adapter, _ := NewStoreAdapter(instance)
		reader, _, err := adapter.GetObject(ctx, "probe", "target")
		return reader, err
	}
	reader, _, err := instance.OpenObject(ctx, "probe", "target")
	return reader, err
}

func benchmarkStreamCompleteness(b *testing.B, item streamBenchmarkCase, opener streamBenchmarkOpener) {
	instance, store := prepareStreamBenchmark(b, item)
	if opener == nil {
		opener = openStreamBenchmark
	}
	buffer := make([]byte, item.chunk)
	want := streamBenchmarkLength(item)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		reader, err := opener(instance, item)
		if err != nil {
			b.Fatal(err)
		}
		if item.seek {
			if _, err := reader.(io.Seeker).Seek(int64(item.size/2), io.SeekStart); err != nil {
				b.Fatal(err)
			}
		}
		read, err := consumeCompletionStream(reader, buffer)
		if read != want || (err != io.EOF && !(item.short && err == io.ErrUnexpectedEOF)) {
			b.Fatalf("read=%d want=%d error=%v", read, want, err)
		}
		if err := reader.Close(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if store.gets != b.N {
		b.Fatal(fmt.Sprintf("opens=%d want=%d", store.gets, b.N))
	}
	if item.short {
		assertSaveBodyCounts(b, store, 1, b.N)
	}
}

func consumeCompletionStream(reader io.Reader, buffer []byte) (int, error) {
	total := 0
	for {
		n, err := reader.Read(buffer)
		total += n
		if err != nil {
			return total, err
		}
	}
}

func prepareStreamBenchmark(b *testing.B, item streamBenchmarkCase) (*Instance, *saveBodyStore) {
	b.Helper()
	store := &saveBodyStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{Backend: BackendMemory}, store, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(context.Background(), "probe"); err != nil {
		b.Fatal(err)
	}
	if _, err := instance.PutObject(context.Background(), "probe", "target", bytes.Repeat([]byte("x"), item.size), PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}}); err != nil {
		b.Fatal(err)
	}
	if item.short {
		store.fault = &saveBodyFault{after: 1, together: true, err: io.EOF}
	}
	return instance, store
}

func streamBenchmarkLength(item streamBenchmarkCase) int {
	if item.short {
		return 1
	}
	if item.seek {
		return item.size - item.size/2
	}
	if item.ranged {
		return item.size - 1
	}
	return item.size
}
