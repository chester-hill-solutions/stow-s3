package fs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func multipartForCapacity(t *testing.T, store *FilesystemStore) string {
	t.Helper()
	upload, err := store.CreateMultipartUpload(context.Background(), "bucket", "key", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return upload.UploadID
}

func TestMultipartConcurrentNamespaceAdmission(t *testing.T) {
	ns := publicationNamespace(t)
	stores := []*FilesystemStore{publicationStore(t, ns, t.TempDir()), publicationStore(t, ns, t.TempDir())}
	ids := []string{multipartForCapacity(t, stores[0]), multipartForCapacity(t, stores[1])}
	start, results := make(chan struct{}), make(chan error, 2)
	var workers sync.WaitGroup
	for i, store := range stores {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := store.UploadPart(context.Background(), ids[i], 1, strings.NewReader(strings.Repeat("x", 1100000)))
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	accepted, refused := 0, 0
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

func TestMultipartSharedHostAdmission(t *testing.T) {
	host := publicationHost(t)
	var stores []*FilesystemStore
	for _, id := range []string{"first", "second"} {
		ns, err := host.Bind(capacity.NamespaceOptions{ID: id, MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, publicationStore(t, ns, t.TempDir()))
	}
	ids := []string{multipartForCapacity(t, stores[0]), multipartForCapacity(t, stores[1])}
	ctx := context.Background()
	body := strings.Repeat("x", 1100000)
	if _, err := stores[0].UploadPart(ctx, ids[0], 1, strings.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := stores[1].UploadPart(ctx, ids[1], 1, strings.NewReader(body)); !errors.Is(err, capacity.ErrFull) {
		t.Fatalf("shared host budget ignored: %v", err)
	}
	if err := stores[0].AbortMultipartUpload(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := stores[1].UploadPart(ctx, ids[1], 1, strings.NewReader(body)); err != nil {
		t.Fatalf("abort did not release shared capacity: %v", err)
	}
}
