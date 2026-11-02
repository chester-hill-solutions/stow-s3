package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestReopenSharesByteQuotaWithMultipartParts(t *testing.T) {
	for _, backend := range []Backend{BackendMemory, BackendFilesystem, BackendWorkspace} {
		for _, cap := range []int64{8, 11, 12, 13} {
			t.Run(fmt.Sprintf("%s/cap=%d", backend, cap), func(t *testing.T) {
				checkReopenByteQuota(t, backend, cap)
			})
		}
	}
}

func checkReopenByteQuota(t *testing.T, backend Backend, cap int64) {
	t.Helper()
	ctx := context.Background()
	open := quotaReopenStore(t, backend)
	original, err := OpenWithStore(Options{Backend: backend, MaxBytes: 16}, open(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	if err := original.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	_, err = original.PutObject(ctx, "assets", "source", []byte("123456"), PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	upload := mustCreateUpload(t, ctx, original, "assets", "source")
	part := mustUploadPart(t, ctx, original, upload, []byte("abcdef"))
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}

	store := open()
	defer store.Close()
	reopened, err := OpenWithStore(Options{Backend: backend, MaxBytes: cap}, store, nil)
	if cap < 12 {
		if reopened != nil {
			defer reopened.Close()
		}
		if !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("open with 6 object + 6 part bytes at cap %d: %v", cap, err)
		}
		// Refusal must leave the same handle usable at a sufficient cap, with
		// no stored object or upload removed to make the lower cap fit.
		reopened, err = OpenWithStore(Options{Backend: backend, MaxBytes: 12}, store, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertReopenedQuotaData(t, reopened, upload, part)
}

func quotaReopenStore(t *testing.T, backend Backend) func() storage.Store {
	t.Helper()
	root := t.TempDir()
	memory := storage.NewMemoryStore()
	return func() storage.Store {
		if backend == BackendMemory {
			return memory
		}
		if backend == BackendFilesystem {
			store, err := fs.NewFilesystemStore(root)
			if err != nil {
				t.Fatal(err)
			}
			return store
		}
		store, err := workspace.New(workspace.Options{Root: root})
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
}

func assertReopenedQuotaData(t *testing.T, instance *Instance, upload string, part *storage.PartInfo) {
	t.Helper()
	ctx := context.Background()
	object, err := instance.GetObject(ctx, "assets", "source")
	if err != nil || string(object.Data) != "123456" || instance.Usage() != (Usage{Bytes: 6, Objects: 1}) {
		t.Fatalf("reopened object=%+v, usage=%+v, error=%v", object, instance.Usage(), err)
	}
	parts, err := instance.ListParts(ctx, upload)
	if err != nil || len(parts) != 1 || parts[0].Size != part.Size || parts[0].ETag != part.ETag {
		t.Fatalf("reopened parts=%+v, error=%v", parts, err)
	}
	if _, err := instance.PutObject(ctx, "assets", "extra", []byte("xx"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("reopened part reservation not enforced: %v", err)
	}
	if err := instance.DeleteObject(ctx, "assets", "source"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part}); err != nil {
		t.Fatal(err)
	}
	object, err = instance.GetObject(ctx, "assets", "source")
	if err != nil || string(object.Data) != "abcdef" || instance.reservedBytes != 0 {
		t.Fatalf("retained multipart payload=%+v, reserved=%d, error=%v", object, instance.reservedBytes, err)
	}
}

// Advertise large logical sizes without allocating huge payloads. This tests
// overflow at the accounting boundary for caller-provided store metadata.
type largeQuotaMetadataStore struct {
	*storage.MemoryStore
	objectSize int64
	partSize   int64
}

func (s *largeQuotaMetadataStore) ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	page, err := s.MemoryStore.ListObjectsV2(ctx, bucket, opts)
	if err == nil {
		for j := range page.Objects {
			page.Objects[j].Size = s.objectSize
		}
	}
	return page, err
}

func (s *largeQuotaMetadataStore) ListParts(ctx context.Context, upload string) ([]storage.PartInfo, error) {
	parts, err := s.MemoryStore.ListParts(ctx, upload)
	for j := range parts {
		parts[j].Size = s.partSize
	}
	return parts, err
}

func TestReopenCombinedByteQuotaDoesNotOverflow(t *testing.T) {
	for _, partSize := range []int64{1, 2} {
		t.Run(fmt.Sprintf("part=%d", partSize), func(t *testing.T) {
			memory := quotaScanFixture(t)
			defer memory.Close()
			store := &largeQuotaMetadataStore{MemoryStore: memory, objectSize: UnlimitedBytes - 1, partSize: partSize}
			instance, err := OpenWithStore(Options{MaxBytes: UnlimitedBytes}, store, nil)
			if instance != nil {
				defer instance.Close()
			}
			if partSize == 2 {
				if !errors.Is(err, ErrQuotaExceeded) {
					t.Fatalf("overflowing combined quota accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func quotaScanFixture(t testing.TB) *storage.MemoryStore {
	t.Helper()
	ctx := context.Background()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "assets", "source", strings.NewReader("123456"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	upload, err := store.CreateMultipartUpload(ctx, "assets", "source", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("abcdef")); err != nil {
		t.Fatal(err)
	}
	return store
}

func BenchmarkQuotaReconciliation(b *testing.B) {
	store := quotaScanFixture(b)
	defer store.Close()
	ctx := context.Background()
	opts := Options{MaxBytes: 12, MaxObjects: 1, MaxMultipartUploads: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		if _, err := scanStoredUsage(ctx, opts, store, store); err != nil {
			b.Fatal(err)
		}
	}
}
