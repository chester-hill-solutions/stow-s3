package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type completionBenchmarkLookup struct {
	storage.MultipartStore
	upload *storage.MultipartUpload
	err    error
}

func (s *completionBenchmarkLookup) GetMultipartUpload(context.Context, string) (*storage.MultipartUpload, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.upload, nil
}

func BenchmarkCompletionLookupAccounting(b *testing.B) {
	unavailable := errors.New("lookup unavailable")
	cases := []struct {
		name string
		err  error
	}{
		{"confirmed-missing", storage.ErrUploadNotFound},
		{"wrapped-missing", fmt.Errorf("lookup: %w", storage.ErrUploadNotFound)},
		{"unknown", unavailable},
		{"marker-first", errors.Join(storage.ErrMutationCommitted, storage.ErrUploadNotFound, unavailable)},
		{"missing-first", errors.Join(storage.ErrUploadNotFound, unavailable, storage.ErrMutationCommitted)},
		{"retained", nil},
	}
	for _, item := range cases {
		b.Run(item.name, func(b *testing.B) { benchmarkCompletionLookup(b, item.err) })
	}
}

func benchmarkCompletionLookup(b *testing.B, lookupErr error) {
	upload := &storage.MultipartUpload{UploadID: "upload", Bucket: "probe", Key: "target"}
	part := &storage.PartInfo{PartNumber: 1, Size: 4}
	meta := &storage.ObjectMeta{Bucket: "probe", Key: "target", Size: 4}
	cause := storage.CommittedError(errors.New("after publication"))
	instance := &Instance{multipartStore: &completionBenchmarkLookup{upload: upload, err: lookupErr}}
	options := Options{MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		instance.usageState = newUsageState(options)
		instance.finishUpload(upload, false, nil)
		instance.finishPart(upload.UploadID, part, nil)
		instance.finishCompletion(upload.UploadID, 0, meta, false, cause)
		if err := instance.reconcileCompletionError(context.Background(), upload.UploadID, cause); !errors.Is(err, storage.ErrMutationCommitted) {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryCompletionSuccessLifecycle(b *testing.B) {
	ctx := context.Background()
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1}, storage.NewMemoryStore(), nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "probe"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		upload, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
		if err != nil {
			b.Fatal(err)
		}
		part, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("body"))
		if err != nil {
			b.Fatal(err)
		}
		meta, err := instance.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part})
		if err != nil || meta.Size != 4 {
			b.Fatalf("completion=%+v error=%v", meta, err)
		}
	}
}
