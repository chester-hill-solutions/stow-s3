package fs

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The listing scale characterization. It seeds a bucket with mixed prefixes and
// measures what one page and one full enumeration cost as the object count
// grows. It is a benchmark rather than a gate: timing stays out of make standards
// because a shared runner cannot measure it, and the point here is to establish
// the count at which the current walk-per-page design stops being acceptable.
func BenchmarkListObjectsV2Scale(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		store, bucket := seedListScale(b, count)
		middle := middleContinuationToken(b, store, bucket)

		b.Run(fmt.Sprintf("count=%d/first-page", count), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				listPage(b, store, bucket, storage.ListOptions{MaxKeys: 1000})
			}
		})
		b.Run(fmt.Sprintf("count=%d/middle-page", count), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				listPage(b, store, bucket, storage.ListOptions{MaxKeys: 1000, ContinuationToken: middle})
			}
		})
		b.Run(fmt.Sprintf("count=%d/prefix", count), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				listPage(b, store, bucket, storage.ListOptions{Prefix: "gamma/", MaxKeys: 1000})
			}
		})
		b.Run(fmt.Sprintf("count=%d/full-enumeration", count), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				enumerateList(b, store, bucket)
			}
		})
	}
}

func seedListScale(b *testing.B, count int) (*FilesystemStore, string) {
	b.Helper()
	store, err := NewFilesystemStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	const bucket = "scale"
	if err := store.CreateBucket(ctx, bucket); err != nil {
		b.Fatal(err)
	}
	// Interleaved prefixes, so a prefix listing filters a scattered set rather
	// than an alphabetically contiguous block the walk could stop after.
	prefixes := []string{"alpha/", "beta/", "gamma/", "delta/", "reports/2026/", "assets/images/"}
	for n := 0; n < count; n++ {
		key := fmt.Sprintf("%s%07d", prefixes[n%len(prefixes)], n)
		if _, err := store.PutObject(ctx, bucket, key, strings.NewReader("x"), storage.PutOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	return store, bucket
}

// middleContinuationToken returns a token five pages into the bucket. The pages
// used to reach it are small, so both the 1,000- and 10,000-object buckets have
// one; the page that is then measured is a full 1,000-key page.
func middleContinuationToken(b *testing.B, store *FilesystemStore, bucket string) string {
	b.Helper()
	ctx := context.Background()
	token := ""
	for page := 0; page < 5; page++ {
		result, err := store.ListObjectsV2(ctx, bucket, storage.ListOptions{MaxKeys: 100, ContinuationToken: token})
		if err != nil {
			b.Fatal(err)
		}
		if !result.IsTruncated {
			b.Fatalf("bucket is smaller than five small pages")
		}
		token = result.NextContinuationToken
	}
	return token
}

func listPage(b *testing.B, store *FilesystemStore, bucket string, opts storage.ListOptions) {
	b.Helper()
	result, err := store.ListObjectsV2(context.Background(), bucket, opts)
	if err != nil {
		b.Fatal(err)
	}
	if len(result.Objects) == 0 {
		b.Fatal("page is empty")
	}
}

func enumerateList(b *testing.B, store *FilesystemStore, bucket string) {
	b.Helper()
	ctx := context.Background()
	token := ""
	for {
		result, err := store.ListObjectsV2(ctx, bucket, storage.ListOptions{MaxKeys: 1000, ContinuationToken: token})
		if err != nil {
			b.Fatal(err)
		}
		if !result.IsTruncated {
			return
		}
		token = result.NextContinuationToken
	}
}
