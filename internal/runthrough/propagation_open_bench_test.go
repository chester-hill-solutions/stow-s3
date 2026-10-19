package runthrough

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type propagationCostReader struct{ closes int }

func (r *propagationCostReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *propagationCostReader) Close() error             { r.closes++; return nil }

type propagationCostStore struct {
	*storage.MemoryStore
	failure error
	reader  io.ReadCloser
}

func (s *propagationCostStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	if s.failure != nil {
		return s.reader, nil, s.failure
	}
	return s.MemoryStore.GetObject(ctx, bucket, key)
}

type propagationCostUpstream struct {
	Client
	etag string
}

func (c *propagationCostUpstream) HeadObject(context.Context, string, string) (*storage.ObjectMeta, error) {
	return nil, storage.ErrObjectNotFound
}
func (c *propagationCostUpstream) PutObject(_ context.Context, _, _ string, reader io.Reader, _ storage.PutOptions) (string, error) {
	_, err := io.Copy(io.Discard, reader)
	return c.etag, err
}
func BenchmarkPropagationSourceOpen(b *testing.B) {
	for _, mode := range []string{"success", "conflict", "nil-error", "reader-error"} {
		for _, reconcile := range []bool{false, true} {
			suffix := "/first"
			if reconcile {
				suffix = "/reconcile"
			}
			b.Run(mode+suffix, func(b *testing.B) { benchmarkPropagationSource(b, mode, reconcile) })
		}
	}
}
func benchmarkPropagationSource(b *testing.B, mode string, reconcile bool) {
	ctx := context.Background()
	store := &propagationCostStore{MemoryStore: storage.NewMemoryStore()}
	if err := store.CreateBucket(ctx, "probe"); err != nil {
		b.Fatal(err)
	}
	meta, err := store.PutObject(ctx, "probe", "target", strings.NewReader("old"), storage.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}})
	if err != nil {
		b.Fatal(err)
	}
	reader := &propagationCostReader{}
	entry := OutboxEntry{Bucket: "probe", Key: "target", Version: meta.VersionID, UpstreamAbsent: true}
	var want error
	switch mode {
	case "conflict":
		entry.Version = "wrong-version"
		want = ErrOutboxVersionConflict
	case "nil-error", "reader-error":
		store.failure = errors.New("source opening refused")
		want = store.failure
	}
	if mode == "reader-error" {
		store.reader = reader
	}
	adapter := &Adapter{local: store, upstream: &propagationCostUpstream{etag: meta.ETag}, outbox: NewMemoryOutbox()}
	b.Cleanup(func() { _ = adapter.Close() })
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if err := adapter.propagateWrite(ctx, entry, reconcile); err != want {
			b.Fatalf("error=%v want=%v", err, want)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(reader.closes)/float64(b.N), "failed-close/op")
}
