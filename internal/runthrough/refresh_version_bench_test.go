package runthrough_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type versionCostOutbox struct {
	runthrough.Outbox
	saves int
}

func (o *versionCostOutbox) SaveUpstreamState(bucket, key string, state runthrough.UpstreamState) error {
	o.saves++
	return o.Outbox.(interface {
		SaveUpstreamState(string, string, runthrough.UpstreamState) error
	}).SaveUpstreamState(bucket, key, state)
}
func (o *versionCostOutbox) UpstreamState(bucket, key string) (runthrough.UpstreamState, bool, error) {
	return o.Outbox.(interface {
		UpstreamState(string, string) (runthrough.UpstreamState, bool, error)
	}).UpstreamState(bucket, key)
}
func BenchmarkRefreshVersionFallback(b *testing.B) {
	for _, kind := range []string{"memory", "file"} {
		for _, changed := range []bool{false, true} {
			name := kind + "/stable"
			if changed {
				name = kind + "/changed"
			}
			b.Run(name, func(b *testing.B) { benchmarkVersionFallback(b, kind, changed) })
		}
	}
}
func benchmarkVersionFallback(b *testing.B, kind string, changed bool) {
	var delegate runthrough.Outbox = runthrough.NewMemoryOutbox()
	if kind == "file" {
		delegate = fileVersionOutbox(b)
	}
	outbox := &versionCostOutbox{Outbox: delegate}
	cache := &versionCostCache{MemoryStore: storage.NewMemoryStore()}
	f := newVersionFixtureWithCache(b, "new", outbox, cache)
	ctx := context.Background()
	f.client.change = nil
	if changed {
		body := "new"
		cache.change = func() error {
			err := versionPut(ctx, f.client.mockUpstream, body)
			if body == "new" {
				body = "old"
			} else {
				body = "new"
			}
			return err
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		reader, _, err := f.adapter.GetObject(ctx, "probe", "target")
		if err != nil {
			b.Fatal(err)
		}
		size, err := io.Copy(io.Discard, reader)
		_ = reader.Close()
		if err != nil || size != 3 {
			b.Fatalf("read size=%d error=%v", size, err)
		}
	}
	b.StopTimer()
	if f.client.gets < b.N || f.client.gets > 2*b.N || f.client.closes != f.client.gets {
		b.Fatalf("gets=%d closes=%d N=%d", f.client.gets, f.client.closes, b.N)
	}
	b.ReportMetric(float64(outbox.saves)/float64(b.N), "state-writes/op")
	b.ReportMetric(float64(f.client.gets)/float64(b.N), "upstream-gets/op")
}

// Change upstream once per eviction in both benchmark variants. A per-GET
// mutation would give a one-GET implementation less fixture work as well.
type versionCostCache struct {
	*storage.MemoryStore
	change func() error
}

func (s *versionCostCache) DeleteObject(ctx context.Context, bucket, key string) error {
	err := s.MemoryStore.DeleteObject(ctx, bucket, key)
	if err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		return err
	}
	if s.change != nil {
		if changeErr := s.change(); changeErr != nil {
			return changeErr
		}
	}
	return err
}

func BenchmarkRefreshVersionControl(b *testing.B) {
	for _, head := range []bool{false, true} {
		name := "cache-hit"
		if head {
			name = "head-evicted"
		}
		b.Run(name, func(b *testing.B) {
			outbox := &versionCostOutbox{Outbox: fileVersionOutbox(b)}
			f := newVersionFixtureWithCache(b, "old", outbox, nil)
			f.client.change = nil
			if !head {
				f.adapter = runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyReadThroughCache}, f.local, f.cache, f.client, outbox)
				reader, _, err := f.adapter.GetObject(context.Background(), "probe", "target")
				if err != nil {
					b.Fatal(err)
				}
				_ = reader.Close()
			}
			f.client.gets, f.client.closes, outbox.saves = 0, 0, 0
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if head {
					if _, err := f.adapter.HeadObject(context.Background(), "probe", "target"); err != nil {
						b.Fatal(err)
					}
				} else {
					reader, _, err := f.adapter.GetObject(context.Background(), "probe", "target")
					if err != nil {
						b.Fatal(err)
					}
					_, err = io.Copy(io.Discard, reader)
					_ = reader.Close()
					if err != nil {
						b.Fatal(err)
					}
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(outbox.saves)/float64(b.N), "state-writes/op")
			b.ReportMetric(float64(f.client.gets)/float64(b.N), "upstream-gets/op")
		})
	}
}
