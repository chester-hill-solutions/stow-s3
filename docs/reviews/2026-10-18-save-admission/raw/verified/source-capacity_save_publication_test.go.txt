package fs

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestSaveCapacityConcurrentAdmission(t *testing.T) {
	namespace := publicationNamespace(t)
	stores := []*FilesystemStore{publicationStore(t, namespace, t.TempDir()), publicationStore(t, namespace, t.TempDir())}
	start, results := make(chan struct{}), make(chan error, 2)
	var writers sync.WaitGroup
	for _, store := range stores {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			_, err := store.PutObject(context.Background(), "bucket", "key", strings.NewReader(strings.Repeat("x", 1<<20)), storage.PutOptions{RequestKey: "concurrent"})
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

func TestSaveCapacitySharesHostBudget(t *testing.T) {
	host := publicationHost(t)
	var stores []*FilesystemStore
	for _, id := range []string{"first", "second"} {
		namespace, err := host.Bind(capacity.NamespaceOptions{ID: id, MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, publicationStore(t, namespace, t.TempDir()))
	}
	ctx := context.Background()
	body := strings.Repeat("x", 1<<20)
	if _, err := stores[0].PutObject(ctx, "bucket", "key", strings.NewReader(body), storage.PutOptions{RequestKey: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := stores[1].PutObject(ctx, "bucket", "key", strings.NewReader(body), storage.PutOptions{RequestKey: "second"}); !errors.Is(err, capacity.ErrFull) {
		t.Fatalf("host quota ignored: %v", err)
	}
	if _, err := stores[1].HeadObject(ctx, "bucket", "key"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("refused save visible: %v", err)
	}
}

func TestSaveCapacityProjectsExactRecordAndReceipt(t *testing.T) {
	for _, key := range []string{"key", strings.Repeat("long-key/", 70)} {
		t.Run(key[:3], func(t *testing.T) { checkSaveCapacityProjection(t, key) })
	}
}

func checkSaveCapacityProjection(t *testing.T, key string) {
	t.Helper()
	ctx := context.Background()
	namespace := publicationNamespace(t)
	store := publicationStore(t, namespace, t.TempDir())
	for _, request := range []string{"first", "overwrite"} {
		if _, err := store.PutObject(ctx, "bucket", key, strings.NewReader(request), storage.PutOptions{RequestKey: request}); err != nil {
			t.Fatal(err)
		}
		usage, err := store.scanCapacityLocked()
		if err != nil {
			t.Fatal(err)
		}
		operation := capacity.Operation{ID: request, Owner: "test", Meaning: request, Dependencies: usage.Resources}
		for i := range operation.Dependencies {
			operation.Dependencies[i].StoreID = store.saves.id
		}
		if err := namespace.BeginOperation(ctx, operation); err != nil {
			t.Fatalf("projected versions differ from native files: %v", err)
		}
		if err := namespace.ReleaseOperation(ctx, request, "test"); err != nil {
			t.Fatal(err)
		}
		before, err := namespace.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.reconcileCapacityLocked(); err != nil {
			t.Fatal(err)
		}
		after, err := namespace.Snapshot(ctx)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("save left stale charge: before=%+v after=%+v err=%v", before, after, err)
		}
	}
	if err := store.DeleteObject(ctx, "bucket", key); err != nil {
		t.Fatal(err)
	}
	snapshot, err := namespace.Snapshot(ctx)
	if err != nil || snapshot.PayloadBytes != 0 {
		t.Fatalf("delete did not release payload: %+v, %v", snapshot, err)
	}
	if _, err := os.Stat(store.pendingPath()); !os.IsNotExist(err) {
		t.Fatalf("terminal save left intent: %v", err)
	}
}
