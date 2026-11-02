package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func bucketCapacityNamespace(t *testing.T, reserve int64) *capacity.Namespace {
	t.Helper()
	host := publicationHost(t)
	ns, err := host.Bind(capacity.NamespaceOptions{ID: "project", MaxBytes: 4 << 20, RecoveryReserveBytes: reserve})
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

func bucketCapacityStore(t *testing.T, ns *capacity.Namespace, dir string) *FilesystemStore {
	t.Helper()
	store, err := NewFilesystemStoreWithNamespace(dir, ns)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func assertBucketCapacityMatchesFiles(t *testing.T, store *FilesystemStore, ns *capacity.Namespace) {
	t.Helper()
	before, err := ns.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.reconcileCapacityLocked(); err != nil {
		t.Fatal(err)
	}
	after, err := ns.Snapshot(context.Background())
	if err != nil || before != after {
		t.Errorf("bucket accounting required later reconciliation: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestBucketCapacityCreationChargesMetadata(t *testing.T) {
	ctx := context.Background()
	ns := bucketCapacityNamespace(t, 1<<20)
	store := bucketCapacityStore(t, ns, t.TempDir())
	if err := store.CreateBucket(ctx, "created"); err != nil {
		t.Fatal(err)
	}
	info, err := store.HeadBucket(ctx, "created")
	if err != nil || info.CreationDate.IsZero() {
		t.Fatalf("created bucket=%+v, %v", info, err)
	}
	assertBucketCapacityMatchesFiles(t, store, ns)
	objects, err := store.ListObjectsV2(ctx, "created", storage.ListOptions{})
	if err != nil || len(objects.Objects) != 0 {
		t.Fatalf("new bucket is not usable and empty: %+v, %v", objects, err)
	}
}

func TestBucketCapacityDeletionReleasesAfterReopen(t *testing.T) {
	ctx := context.Background()
	ns := bucketCapacityNamespace(t, 1<<20)
	dir := t.TempDir()
	store := bucketCapacityStore(t, ns, dir)
	if err := store.CreateBucket(ctx, "deleted"); err != nil {
		t.Fatal(err)
	}
	original, err := store.HeadBucket(ctx, "deleted")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := bucketCapacityStore(t, ns, dir)
	observed, err := reopened.HeadBucket(ctx, "deleted")
	if err != nil || !observed.CreationDate.Equal(original.CreationDate) {
		t.Fatalf("reopen changed bucket metadata: %+v, %v", observed, err)
	}
	before, err := ns.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.DeleteBucket(ctx, "deleted"); err != nil {
		t.Fatal(err)
	}
	after, err := ns.Snapshot(ctx)
	if err != nil || after.RecoveryBytes >= before.RecoveryBytes {
		t.Errorf("bucket deletion retained metadata charge: before=%+v after=%+v err=%v", before, after, err)
	}
	assertBucketCapacityMatchesFiles(t, reopened, ns)
	if _, err := reopened.HeadBucket(ctx, "deleted"); !errors.Is(err, storage.ErrBucketNotFound) {
		t.Fatalf("deleted bucket visible: %v", err)
	}
}

func TestBucketCapacityRefusalLeavesNoBucket(t *testing.T) {
	ctx := context.Background()
	ns := bucketCapacityNamespace(t, 16<<10)
	store := bucketCapacityStore(t, ns, t.TempDir())
	for n := 0; n < 128; n++ {
		name := fmt.Sprintf("bucket-%03d", n)
		err := store.CreateBucket(ctx, name)
		if errors.Is(err, capacity.ErrFull) {
			assertRefusedCapacityBucket(t, store, name, n)
			if n == 0 {
				t.Fatal("no small bucket was admitted")
			}
			if err := store.DeleteBucket(ctx, "bucket-000"); err != nil {
				t.Fatal(err)
			}
			if err := store.CreateBucket(ctx, "bucket-retry"); err != nil {
				t.Fatalf("released metadata capacity was unavailable: %v", err)
			}
			assertBucketCapacityMatchesFiles(t, store, ns)
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("bucket creation bypassed the namespace recovery reserve")
}

func assertRefusedCapacityBucket(t *testing.T, store *FilesystemStore, name string, accepted int) {
	t.Helper()
	ctx := context.Background()
	if _, err := os.Stat(store.bucketDir(name)); !os.IsNotExist(err) {
		t.Errorf("quota refusal left a bucket directory: %v", err)
	}
	if _, err := store.HeadBucket(ctx, name); !errors.Is(err, storage.ErrBucketNotFound) {
		t.Errorf("quota refusal left a visible bucket: %v", err)
	}
	buckets, err := store.ListBuckets(ctx)
	if err != nil || len(buckets) != accepted {
		t.Errorf("refused bucket appeared in listing: listed=%d accepted=%d err=%v", len(buckets), accepted, err)
	}
}

func TestBucketCapacityHoldPreventsDeletion(t *testing.T) {
	ctx := context.Background()
	ns := bucketCapacityNamespace(t, 1<<20)
	store := bucketCapacityStore(t, ns, t.TempDir())
	if err := store.CreateBucket(ctx, "held"); err != nil {
		t.Fatal(err)
	}
	if err := store.reconcileCapacityLocked(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.bucketDir("held"), "bucket.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ref := resourceFor(path, data, capacity.Recovery)
	ref.StoreID = store.saves.id
	if err := ns.BeginOperation(ctx, capacity.Operation{ID: "hold", Owner: "owner", Meaning: "bucket-metadata", Dependencies: []capacity.Resource{ref}}); err != nil {
		t.Fatal(err)
	}
	err = store.DeleteBucket(ctx, "held")
	if !errors.Is(err, capacity.ErrHeld) {
		t.Errorf("held bucket was deleted: %v", err)
	}
	if _, err := store.HeadBucket(ctx, "held"); err != nil {
		t.Errorf("held bucket disappeared: %v", err)
	}
	if err := ns.ReleaseOperation(ctx, "hold", "owner"); err != nil {
		t.Fatal(err)
	}
	if err == nil {
		return
	}
	if err := store.DeleteBucket(ctx, "held"); err != nil {
		t.Fatal(err)
	}
}
