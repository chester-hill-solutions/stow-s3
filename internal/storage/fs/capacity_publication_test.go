package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func publicationNamespace(t *testing.T) *capacity.Namespace {
	t.Helper()
	host, err := capacity.Open(capacity.HostOptions{Dir: filepath.Join(t.TempDir(), "host"), MaxBytes: 5 << 20})
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := host.Bind(capacity.NamespaceOptions{ID: "project", MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return namespace
}

func publicationStore(t *testing.T, namespace *capacity.Namespace, dir string) *FilesystemStore {
	t.Helper()
	store, err := NewFilesystemStoreWithNamespace(dir, namespace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestOrdinaryWritesShareNamespaceAdmission(t *testing.T) {
	namespace := publicationNamespace(t)
	stores := []*FilesystemStore{publicationStore(t, namespace, t.TempDir()), publicationStore(t, namespace, t.TempDir())}
	start := make(chan struct{})
	results := make(chan error, 2)
	var writers sync.WaitGroup
	for _, store := range stores {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			_, err := store.PutObject(context.Background(), "bucket", "key", strings.NewReader(strings.Repeat("x", 1<<20)), storage.PutOptions{})
			results <- err
		}()
	}
	close(start)
	writers.Wait()
	close(results)
	var accepted, refused int
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, capacity.ErrFull) {
			refused++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || refused != 1 {
		t.Fatalf("accepted=%d refused=%d", accepted, refused)
	}
}

func TestOrdinaryOverwriteAndReopenChargeOneRecord(t *testing.T) {
	ctx := context.Background()
	namespace := publicationNamespace(t)
	dir := t.TempDir()
	store := publicationStore(t, namespace, dir)
	for _, body := range []string{"first", "other"} {
		if _, err := store.PutObject(ctx, "bucket", "key", strings.NewReader(body), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		assertPublicationCharge(t, store, namespace)
	}
	before, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFilesystemStoreWithNamespace(dir, namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.PayloadBytes != before.PayloadBytes || after.PendingBytes != 0 {
		t.Fatalf("reopen changed charge: before=%+v after=%+v", before, after)
	}
	if err := reopened.DeleteObject(ctx, "bucket", "key"); err != nil {
		t.Fatal(err)
	}
	deleted, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.PayloadBytes != 0 {
		t.Fatalf("deleted charge=%+v", deleted)
	}
}

func TestOrdinaryWritesRespectSharedHostBudget(t *testing.T) {
	host, err := capacity.Open(capacity.HostOptions{Dir: filepath.Join(t.TempDir(), "host"), MaxBytes: 5 << 20})
	if err != nil {
		t.Fatal(err)
	}
	var stores []*FilesystemStore
	for _, id := range []string{"first", "second"} {
		namespace, err := host.Bind(capacity.NamespaceOptions{ID: id, MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, publicationStore(t, namespace, t.TempDir()))
	}
	ctx := context.Background()
	if _, err := stores[0].PutObject(ctx, "bucket", "key", strings.NewReader(strings.Repeat("x", 1<<20)), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := stores[1].PutObject(ctx, "bucket", "key", strings.NewReader(strings.Repeat("x", 1<<20)), storage.PutOptions{}); !errors.Is(err, capacity.ErrFull) {
		t.Fatalf("host budget ignored: %v", err)
	}
	if _, err := stores[1].HeadObject(ctx, "bucket", "key"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("refused write published bytes: %v", err)
	}
}

func assertPublicationCharge(t *testing.T, store *FilesystemStore, namespace *capacity.Namespace) {
	t.Helper()
	ctx := context.Background()
	data, err := os.ReadFile(store.objectPath("bucket", "key"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PayloadBytes != int64(len(data)) || snapshot.PendingBytes != 0 {
		t.Fatalf("charge=%+v record=%d", snapshot, len(data))
	}
}
