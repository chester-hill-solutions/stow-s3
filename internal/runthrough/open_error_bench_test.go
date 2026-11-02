package runthrough_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type openingCostReader struct{ closes int }

func (r *openingCostReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *openingCostReader) Close() error             { r.closes++; return nil }

type openingCostStore struct {
	*storage.MemoryStore
	failure             error
	reader              *openingCostReader
	nilReader, missHead bool
}

func (s *openingCostStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	if s.failure == nil {
		return s.MemoryStore.GetObject(ctx, bucket, key)
	}
	if s.nilReader {
		return nil, nil, s.failure
	}
	return s.reader, nil, s.failure
}
func (s *openingCostStore) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	if s.missHead {
		return nil, storage.ErrObjectNotFound
	}
	return s.MemoryStore.HeadObject(ctx, bucket, key)
}

type openingCostClient struct {
	*mockUpstream
	store *openingCostStore
}

func (c *openingCostClient) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	return c.store.GetObject(ctx, bucket, key)
}

type openingCostCase struct {
	name, path               string
	size                     int
	failure, nilReader, head bool
}

func BenchmarkRunthroughReadOwnership(b *testing.B) {
	for _, item := range []openingCostCase{
		{name: "local", path: "local", size: 3},
		{name: "cached", path: "cached", size: 3},
		{name: "refresh", path: "refresh", size: 3},
		{name: "head-refresh", path: "refresh", size: 3, head: true},
		{name: "local-64KiB", path: "local", size: 64 << 10},
		{name: "local-reader-error", path: "local", failure: true},
		{name: "local-nil-error", path: "local", failure: true, nilReader: true},
		{name: "upstream-reader-error", path: "refresh", failure: true},
		{name: "upstream-nil-error", path: "refresh", failure: true, nilReader: true},
	} {
		b.Run(item.name, func(b *testing.B) { benchmarkOpeningCost(b, item) })
	}
}
func seedOpeningCost(b *testing.B, store *openingCostStore, size int) {
	b.Helper()
	ctx := context.Background()
	if _, err := store.PutObject(ctx, "probe", "target", strings.NewReader(strings.Repeat("x", size)), storage.PutOptions{}); err != nil {
		b.Fatal(err)
	}
}
func prepareOpeningCost(b *testing.B, item openingCostCase) (*runthrough.Adapter, *openingCostStore) {
	b.Helper()
	local := &openingCostStore{MemoryStore: storage.NewMemoryStore()}
	cache := &openingCostStore{MemoryStore: storage.NewMemoryStore(), missHead: item.path == "refresh"}
	upstream := &openingCostStore{MemoryStore: storage.NewMemoryStore()}
	for _, s := range []*openingCostStore{local, cache, upstream} {
		if err := s.CreateBucket(context.Background(), "probe"); err != nil {
			b.Fatal(err)
		}
	}
	seedOpeningCost(b, upstream, item.size)
	fault := upstream
	switch item.path {
	case "local":
		seedOpeningCost(b, local, item.size)
		fault = local
	case "cached":
		seedOpeningCost(b, cache, item.size)
	}
	fault.reader = &openingCostReader{}
	fault.nilReader = item.nilReader
	if item.failure {
		fault.failure = errors.New("opening refused")
	}
	adapter := runthrough.NewWithCache(runthrough.Config{Policy: runthrough.PolicyReadThroughCache}, local, cache, &openingCostClient{newMockUpstream(), upstream})
	b.Cleanup(func() { _ = adapter.Close(); _ = upstream.Close() })
	return adapter, fault
}
func benchmarkOpeningCost(b *testing.B, item openingCostCase) {
	adapter, fault := prepareOpeningCost(b, item)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if item.head {
			if _, err := adapter.HeadObject(ctx, "probe", "target"); err != nil {
				b.Fatal(err)
			}
			continue
		}
		reader, _, err := adapter.GetObject(ctx, "probe", "target")
		if item.failure {
			if err != fault.failure || reader != nil {
				b.Fatalf("opening error: reader=%v error=%v", reader, err)
			}
			continue
		}
		if err != nil {
			b.Fatal(err)
		}
		copied, err := io.Copy(io.Discard, reader)
		_ = reader.Close()
		if err != nil || copied != int64(item.size) {
			b.Fatalf("copied=%d error=%v", copied, err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(fault.reader.closes)/float64(b.N), "failed-close/op")
}
