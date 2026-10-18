package fs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// This is the existing namespace-wide resource ceiling, not a per-store limit.
const filesystemNamespaceResourceLimit = 4096

func resourceCountFilesystemStores(t *testing.T) (*capacity.Namespace, [2]*FilesystemStore) {
	t.Helper()
	if !(&FilesystemStore{}).SupportsGuardedWrites() {
		t.Skip("requires native namespace admission")
	}
	host, err := capacity.Open(capacity.HostOptions{Dir: filepath.Join(t.TempDir(), "host"), MaxBytes: 128 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ns, err := host.Bind(capacity.NamespaceOptions{ID: "files", MaxBytes: 64 << 20, RecoveryReserveBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	var stores [2]*FilesystemStore
	existing := len(stores) // Each binding adds one metadata file after bootstrap.
	for i := range stores {
		store, err := NewFilesystemStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = store
		t.Cleanup(func() { _ = store.Close() })
		usage, err := store.scanCapacityLocked()
		if err != nil {
			t.Fatal(err)
		}
		existing += len(usage.Resources)
	}
	remaining := filesystemNamespaceResourceLimit - existing
	fillResourceCountBuckets(t, stores, remaining)
	total := 0
	for _, store := range stores {
		if err := store.BindNamespace(ns); err != nil {
			t.Fatal(err)
		}
		if err := store.reconcileCapacityLocked(); err != nil {
			t.Fatal(err)
		}
		usage, err := store.scanCapacityLocked()
		if err != nil {
			t.Fatal(err)
		}
		total += len(usage.Resources)
	}
	if total != filesystemNamespaceResourceLimit {
		t.Fatalf("native fixture resource count=%d", total)
	}
	t.Logf("two native stores hold %d actual resources", total)
	return ns, stores
}

func fillResourceCountBuckets(t *testing.T, stores [2]*FilesystemStore, remaining int) {
	t.Helper()
	results := make(chan error, len(stores))
	for i, store := range stores {
		count := remaining / len(stores)
		if i == 0 {
			count += remaining % len(stores)
		}
		go func() { results <- seedResourceCountBuckets(store, count) }()
	}
	var bootstrapErr error
	for range stores {
		if err := <-results; err != nil {
			bootstrapErr = err
		}
	}
	if bootstrapErr != nil {
		t.Fatal(bootstrapErr)
	}
}

func seedResourceCountBuckets(store *FilesystemStore, count int) error {
	for n := 0; n < count; n++ {
		if err := store.CreateBucket(context.Background(), fmt.Sprintf("bucket-%04d", n)); err != nil {
			return err
		}
	}
	return nil
}

func TestResourceCountFilesystemRefusalLeavesNoBucket(t *testing.T) {
	ns, stores := resourceCountFilesystemStores(t)
	ctx := context.Background()
	host, _ := ns.Binding()
	ledger := filepath.Join(host, "capacity.json")
	before, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	err = stores[0].CreateBucket(ctx, "overflow")
	if !errors.Is(err, capacity.ErrFull) {
		t.Errorf("resource-limit bucket result=%v", err)
	}
	if _, err := stores[0].HeadBucket(ctx, "overflow"); !errors.Is(err, storage.ErrBucketNotFound) {
		t.Errorf("resource-limit refusal published bucket: %v", err)
	}
	if _, err := os.Stat(stores[0].bucketDir("overflow")); !os.IsNotExist(err) {
		t.Errorf("resource-limit refusal left bucket directory: %v", err)
	}
	after, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("native resource-limit refusal changed durable ledger")
	}
	dir := stores[0].dataDir
	if err := stores[0].Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFilesystemStoreWithNamespace(dir, ns)
	if err != nil {
		t.Errorf("resource-limit refusal prevented reopen: %v", err)
		return
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := stores[1].DeleteBucket(ctx, "bucket-0000"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.CreateBucket(ctx, "overflow"); err != nil {
		t.Fatalf("native released resource slot unavailable: %v", err)
	}
	assertBucketCapacityMatchesFiles(t, reopened, ns)
	assertBucketCapacityMatchesFiles(t, stores[1], ns)
}
