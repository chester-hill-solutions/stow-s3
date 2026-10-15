package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func BenchmarkRuntimeMultipartValidation(b *testing.B) {
	for _, entries := range []int{1, 64} {
		b.Run(fmt.Sprintf("success/metadata=%d", entries), func(b *testing.B) {
			instance, id := benchmarkValidationInstance(b, entries)
			benchmarkValidationLoop(b, instance, id, "target", nil)
		})
	}
	b.Run("wrong-key", func(b *testing.B) {
		instance, id := benchmarkValidationInstance(b, 1)
		benchmarkValidationLoop(b, instance, id, "other", storage.ErrNoSuchUpload)
	})
	b.Run("unknown-id", func(b *testing.B) {
		instance, _ := benchmarkValidationInstance(b, 1)
		benchmarkValidationLoop(b, instance, "unknown", "target", storage.ErrNoSuchUpload)
	})
}

func benchmarkValidationInstance(b *testing.B, entries int) (*Instance, string) {
	b.Helper()
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1}, storage.NewMemoryStore(), nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(context.Background(), "probe"); err != nil {
		b.Fatal(err)
	}
	metadata := make(map[string]string)
	for n := 0; n < entries; n++ {
		metadata[fmt.Sprint(n)] = "v"
	}
	upload, err := instance.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{Metadata: metadata})
	if err != nil {
		b.Fatal(err)
	}
	return instance, upload.UploadID
}

func benchmarkValidationLoop(b *testing.B, instance *Instance, id, key string, want error) {
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if err := instance.ValidateMultipartUpload(context.Background(), id, "probe", key); err != want {
			b.Fatal(err)
		}
	}
}

func BenchmarkMultipartValidationRecoveryAccounting(b *testing.B) {
	for _, exists := range []bool{false, true} {
		b.Run(fmt.Sprintf("exists=%t", exists), func(b *testing.B) { benchmarkValidationRecovery(b, exists) })
	}
}

func benchmarkValidationRecovery(b *testing.B, exists bool) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(ctx, "probe"); err != nil {
		b.Fatal(err)
	}
	usage := Usage{}
	if exists {
		if _, err := store.PutObject(ctx, "probe", "target", strings.NewReader("old"), storage.PutOptions{}); err != nil {
			b.Fatal(err)
		}
		usage = Usage{Bytes: 3, Objects: 1}
	}
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1}, store, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = instance.Close() })
	upload := &storage.MultipartUpload{UploadID: "missing", Bucket: "probe", Key: "target"}
	part := &storage.PartInfo{PartNumber: 1, Size: 4}
	unknown := errors.New("abort acknowledgement unavailable")
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		instance.usageState = newUsageState(instance.options)
		instance.usage = usage
		instance.finishUpload(upload, exists, nil)
		instance.finishPart(upload.UploadID, part, nil)
		instance.finishAbort(upload.UploadID, exists, unknown)
		if err := instance.ValidateMultipartUpload(ctx, upload.UploadID, "probe", "target"); err != storage.ErrNoSuchUpload {
			b.Fatal(err)
		}
	}
}
