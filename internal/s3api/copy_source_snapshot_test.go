package s3api_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type swappingCopySource struct {
	storage.Store
	originalETag                  string
	originalTime, replacementTime time.Time
	swapped                       bool
	opened, closed                int
}

func (s *swappingCopySource) capture(ctx context.Context, meta *storage.ObjectMeta) error {
	if meta.Key != "source" {
		return nil
	}
	meta.LastModified = s.replacementTime
	if meta.ETag == s.originalETag {
		meta.LastModified = s.originalTime
	}
	if s.swapped {
		return nil
	}
	s.swapped = true
	_, err := s.Store.PutObject(ctx, "bucket", "source", strings.NewReader("replacement source"), storage.PutOptions{
		ContentType: "application/json", Metadata: map[string]string{"generation": "two"},
		ChecksumAlgorithm: "SHA256", ComputeChecksum: true,
	})
	return err
}

func (s *swappingCopySource) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	meta, err := s.Store.HeadObject(ctx, bucket, key)
	if err == nil {
		err = s.capture(ctx, meta)
	}
	return meta, err
}

func (s *swappingCopySource) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	rc, meta, err := s.Store.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	if err := s.capture(ctx, meta); err != nil {
		_ = rc.Close()
		return nil, nil, err
	}
	s.opened++
	return &countedCopyReader{ReadCloser: rc, closed: &s.closed}, meta, nil
}

type countedCopyReader struct {
	io.ReadCloser
	closed *int
}

func (r *countedCopyReader) Close() error {
	*r.closed++
	return r.ReadCloser.Close()
}

func TestConditionalCopyFallbackCapturesTheCheckedSource(t *testing.T) {
	for _, profile := range []string{"direct", "run-through", "runtime"} {
		for _, condition := range []string{"If-Match", "If-None-Match", "If-Unmodified-Since", "If-Modified-Since"} {
			for _, existing := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/existing=%t", profile, condition, existing), func(t *testing.T) {
					checkSwappingCopySource(t, profile, condition, existing)
				})
			}
		}
	}
}

func checkSwappingCopySource(t *testing.T, profile, condition string, existing bool) {
	t.Helper()
	ctx := context.Background()
	memory := storage.NewMemoryStore()
	if err := memory.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	original, err := memory.PutObject(ctx, "bucket", "source", strings.NewReader("captured source"), storage.PutOptions{
		ContentType: "text/plain", Metadata: map[string]string{"generation": "one"},
		ChecksumAlgorithm: "SHA256", ComputeChecksum: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if existing {
		if _, err := memory.PutObject(ctx, "bucket", "target", strings.NewReader("keep destination"), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := time.Date(2026, time.October, 15, 13, 0, 0, 0, time.UTC)
	source := &swappingCopySource{Store: memory, originalETag: original.ETag, originalTime: cutoff.Add(-time.Hour), replacementTime: cutoff.Add(time.Hour)}
	value := swappingCopyCondition(source, condition, cutoff)
	server := swappingCopyServer(t, source, profile)
	request := httptest.NewRequest(http.MethodPut, "/bucket/target", nil)
	request.Header.Set("X-Amz-Copy-Source", "/bucket/source")
	request.Header.Set("X-Amz-Copy-Source-"+condition, value)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if !source.swapped {
		t.Fatal("test did not replace source between observation and copy")
	}
	if source.opened != source.closed {
		t.Errorf("source readers opened=%d, closed=%d", source.opened, source.closed)
	}
	if response.Code == http.StatusPreconditionFailed {
		assertRefusedSnapshotCopy(t, memory, existing)
		return
	}
	if response.Code != http.StatusOK {
		t.Fatalf("COPY: %d %s", response.Code, response.Body.String())
	}
	assertCopyAdapterDestination(t, memory, "target", "COPY", http.StatusOK)
	copied, err := memory.HeadObject(ctx, "bucket", "target")
	if err != nil {
		t.Fatal(err)
	}
	if copied.ChecksumAlgorithm != original.ChecksumAlgorithm || copied.ChecksumValue != original.ChecksumValue {
		t.Errorf("COPY changed captured checksum: %+v, source %+v", copied, original)
	}
}

func swappingCopyCondition(source *swappingCopySource, condition string, cutoff time.Time) string {
	switch condition {
	case "If-Match":
		return source.originalETag
	case "If-None-Match":
		return storage.ETagForBytes([]byte("replacement source"))
	case "If-Modified-Since":
		source.originalTime, source.replacementTime = source.replacementTime, source.originalTime
	}
	return cutoff.Format(http.TimeFormat)
}

func swappingCopyServer(t *testing.T, source storage.Store, profile string) *s3api.Server {
	t.Helper()
	if profile == "run-through" {
		adapter := runthrough.New(runthrough.Config{}, source, nil)
		t.Cleanup(func() { _ = adapter.Close() })
		source = adapter
	}
	if profile == "runtime" {
		instance, err := stowruntime.OpenWithStore(stowruntime.Options{MaxBytes: 1 << 20}, source, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = instance.Close() })
		source, err = stowruntime.NewStoreAdapter(instance)
		if err != nil {
			t.Fatal(err)
		}
	}
	server, err := s3api.New(s3api.Config{Store: source, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func assertRefusedSnapshotCopy(t *testing.T, store storage.Store, existing bool) {
	t.Helper()
	if !existing {
		if _, err := store.HeadObject(context.Background(), "bucket", "target"); !errors.Is(err, storage.ErrObjectNotFound) {
			t.Fatalf("refused COPY destination: %v", err)
		}
		return
	}
	assertCopyAdapterDestination(t, store, "target", "COPY", http.StatusPreconditionFailed)
}

func TestConditionalCopyFallbackAdmitsCapturedSourceSize(t *testing.T) {
	ctx := context.Background()
	memory := storage.NewMemoryStore()
	if err := memory.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	original, err := memory.PutObject(ctx, "bucket", "source", strings.NewReader("tiny"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	source := &swappingCopySource{Store: memory, originalETag: original.ETag}
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{MaxBytes: 16}, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	_, err = instance.CopyObjectCond(ctx, storage.CopyRequest{
		SourceBucket: "bucket", SourceKey: "source", DestBucket: "bucket", DestKey: "target",
		Options: storage.CopyOptions{SourceIfNoneMatch: "\"unrelated\""},
	})
	if !source.swapped {
		t.Fatal("test did not grow source between quota observation and copy")
	}
	if errors.Is(err, stowruntime.ErrQuotaExceeded) {
		if _, err := memory.HeadObject(ctx, "bucket", "target"); !errors.Is(err, storage.ErrObjectNotFound) {
			t.Errorf("oversized COPY published destination: %v", err)
		}
	} else {
		if err != nil {
			t.Fatal(err)
		}
		copied, err := memory.HeadObject(ctx, "bucket", "target")
		if err != nil {
			t.Fatal(err)
		}
		if copied.Size != original.Size {
			t.Errorf("COPY admitted %d bytes against a %d-byte observation", copied.Size, original.Size)
		}
		if usage := instance.Usage(); usage.Bytes > 16 {
			t.Errorf("COPY exceeded runtime quota: %+v", usage)
		}
	}
	if source.opened != source.closed {
		t.Errorf("source readers opened=%d, closed=%d", source.opened, source.closed)
	}
}
