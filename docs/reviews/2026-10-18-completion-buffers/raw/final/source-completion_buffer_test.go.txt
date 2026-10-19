package fs_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// Completion should not retain the parts as well as their assembled payload.
func TestCompletionBufferAllocation(t *testing.T) {
	for _, bound := range []bool{false, true} {
		for _, split := range []bool{false, true} {
			t.Run(fmt.Sprintf("bound=%t/split=%t", bound, split), func(t *testing.T) {
				runCompletionBuffer(t, bound, split)
			})
		}
	}
}

func runCompletionBuffer(t *testing.T, bound, split bool) {
	t.Helper()
	const size = int64(8 << 20)
	_, digest := partHashes(size)
	store, ns, path := partStore(t, bound, 128<<20)
	ctx := context.Background()
	u := partUpload(t, store)
	sizes := []int64{size}
	if split {
		sizes = []int64{5 << 20, 3 << 20}
	}
	parts := make([]storage.PartInfo, 0, len(sizes))
	for i, n := range sizes {
		part, err := store.UploadPart(ctx, u.UploadID, i+1, &partRepeatReader{remaining: n})
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, *part)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	meta, err := store.CompleteMultipartUpload(ctx, u.UploadID, parts)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	budget := uint64(10 << 20)

	t.Logf("completion bytes=%d allocated=%d budget=%d", size, allocated, budget)
	if allocated > budget {
		t.Errorf("completion retained a duplicate payload: allocated=%d budget=%d", allocated, budget)
	}
	etags := make([]string, len(parts))
	for i, p := range parts {
		etags[i] = p.ETag
	}
	if meta.Size != size || meta.ETag != storage.CompletionETag(etags) || meta.Metadata["label"] != "part" {
		t.Fatalf("metadata %+v", meta)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	verifyCompletionBuffer(t, path, ns, meta, digest)
}
