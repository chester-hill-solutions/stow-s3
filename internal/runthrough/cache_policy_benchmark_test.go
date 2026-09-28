package runthrough

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkCacheEvictionPlanner measures the in-memory scan and ordering work
// independently of provider latency. No eviction is needed, so the benchmark
// exercises the normal planner path without store deletes.
func BenchmarkCacheEvictionPlanner(b *testing.B) {
	for _, depth := range []int{40, 160, 1_000, 10_000} {
		b.Run(fmt.Sprintf("entries_%d", depth), func(b *testing.B) {
			adapter := &Adapter{
				cfg:           Config{Cache: CachePolicy{MaxBytes: int64(depth * 1024)}},
				cacheEntries:  make(map[string]cacheEntry, depth),
				separateCache: true,
			}
			for index := 0; index < depth; index++ {
				key := fmt.Sprintf("object-%08d", index)
				adapter.cacheEntries[cacheEntryKey("bucket", key)] = cacheEntry{
					accessedAt: time.Unix(int64(index), 0),
					bucket:     "bucket",
					key:        key,
					size:       1,
				}
			}
			b.ReportAllocs()
			b.ReportMetric(float64(depth), "entries")
			b.ResetTimer()
			for range b.N {
				if err := adapter.evictCache(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
