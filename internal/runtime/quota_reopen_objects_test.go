package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestReopenSharesObjectQuotaWithMultipartTargets(t *testing.T) {
	for _, backend := range []Backend{BackendMemory, BackendFilesystem, BackendWorkspace} {
		for _, uploadsFirst := range []bool{false, true} {
			for _, cap := range []int64{2, 3, 4} {
				t.Run(fmt.Sprintf("%s/uploadsFirst=%t/cap=%d", backend, uploadsFirst, cap), func(t *testing.T) {
					checkReopenObjectQuota(t, backend, uploadsFirst, cap)
				})
			}
		}
	}
}

type objectQuotaFixture struct {
	uploadBucket string
	objectBucket string
	uploads      []string
}

func checkReopenObjectQuota(t *testing.T, backend Backend, uploadsFirst bool, cap int64) {
	t.Helper()
	ctx := context.Background()
	open := quotaReopenStore(t, backend)
	original, err := OpenWithStore(Options{Backend: backend, MaxBytes: 64, MaxObjects: 3}, open(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	fixture := objectQuotaFixture{uploadBucket: "zzz", objectBucket: "aaa"}
	if uploadsFirst {
		fixture.uploadBucket, fixture.objectBucket = "aaa", "zzz"
	}
	for _, bucket := range []string{"aaa", "zzz"} {
		mustCreateBucket(t, ctx, original, bucket)
	}
	for _, key := range []string{"one", "two"} {
		if _, err := original.PutObject(ctx, fixture.objectBucket, key, []byte(key), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		upload := mustCreateUpload(t, ctx, original, fixture.uploadBucket, "pending")
		mustUploadPart(t, ctx, original, upload, []byte("part"))
		fixture.uploads = append(fixture.uploads, upload)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	store := open()
	defer store.Close()
	reopened, err := OpenWithStore(Options{Backend: backend, MaxBytes: 64, MaxObjects: cap}, store, nil)
	if cap == 2 {
		if reopened != nil {
			defer reopened.Close()
		}
		if !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("open with 2 objects + 1 reserved target under cap 2: %v", err)
		}
		reopened, err = OpenWithStore(Options{Backend: backend, MaxBytes: 64, MaxObjects: 3}, store, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertReopenedObjectQuota(t, reopened, fixture)
}

func assertReopenedObjectQuota(t *testing.T, instance *Instance, fixture objectQuotaFixture) {
	t.Helper()
	ctx := context.Background()
	for _, key := range []string{"one", "two"} {
		object, err := instance.GetObject(ctx, fixture.objectBucket, key)
		if err != nil || string(object.Data) != key {
			t.Fatalf("retained object %s: %+v, %v", key, object, err)
		}
	}
	for _, upload := range fixture.uploads {
		parts, err := instance.ListParts(ctx, upload)
		if err != nil || len(parts) != 1 || parts[0].ETag != storage.ETagForBytes([]byte("part")) {
			t.Fatalf("retained upload %s: %+v, %v", upload, parts, err)
		}
	}
	if instance.Usage().Objects != 2 || instance.reservedObjects != 1 {
		t.Fatalf("repeated target accounting: usage=%+v reserved=%d", instance.Usage(), instance.reservedObjects)
	}
	publishReservedObjectTarget(t, instance, fixture)
}

func publishReservedObjectTarget(t *testing.T, instance *Instance, fixture objectQuotaFixture) {
	t.Helper()
	ctx := context.Background()
	if _, err := instance.PutObject(ctx, fixture.uploadBucket, "pending", []byte("published"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, upload := range fixture.uploads {
		if err := instance.AbortMultipartUpload(ctx, upload); err != nil {
			t.Fatal(err)
		}
	}
	object, err := instance.GetObject(ctx, fixture.uploadBucket, "pending")
	if err != nil || string(object.Data) != "published" || instance.Usage().Objects != 3 || instance.reservedObjects != 0 {
		t.Fatalf("target publication/abort: object=%+v usage=%+v reserved=%d error=%v", object, instance.Usage(), instance.reservedObjects, err)
	}
}

type largeObjectQuotaStore struct{ *storage.MemoryStore }

func (s *largeObjectQuotaStore) ScanObjectUsage(_ context.Context, bucket string) (storage.ObjectUsage, error) {
	if bucket == "zzz" {
		return storage.ObjectUsage{Objects: UnlimitedObjects - 1}, nil
	}
	return storage.ObjectUsage{}, nil
}

func TestReopenCombinedObjectQuotaDoesNotOverflow(t *testing.T) {
	ctx := context.Background()
	for _, targets := range []int{1, 2} {
		t.Run(fmt.Sprintf("reserved=%d", targets), func(t *testing.T) {
			store := &largeObjectQuotaStore{MemoryStore: storage.NewMemoryStore()}
			defer store.Close()
			for _, bucket := range []string{"aaa", "zzz"} {
				if err := store.CreateBucket(ctx, bucket); err != nil {
					t.Fatal(err)
				}
			}
			for j := range targets {
				if _, err := store.CreateMultipartUpload(ctx, "aaa", fmt.Sprintf("target-%d", j), storage.MultipartOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			instance, err := OpenWithStore(Options{MaxObjects: UnlimitedObjects}, store, nil)
			if instance != nil {
				defer instance.Close()
			}
			if targets == 2 {
				if !errors.Is(err, ErrQuotaExceeded) {
					t.Fatalf("overflowing object quota accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
