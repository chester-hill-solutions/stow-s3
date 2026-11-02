package fs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
)

func TestBucketCapacityConcurrentCreationRespectsReserve(t *testing.T) {
	ctx := context.Background()
	ns := bucketCapacityNamespace(t, 16<<10)
	stores := []*FilesystemStore{bucketCapacityStore(t, ns, t.TempDir()), bucketCapacityStore(t, ns, t.TempDir())}
	results := make(chan error, 32)
	var workers sync.WaitGroup
	for i, store := range stores {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 16; n++ {
				results <- store.CreateBucket(ctx, fmt.Sprintf("bucket-%d-%03d", i, n))
			}
		}()
	}
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
	if accepted == 0 || refused == 0 {
		t.Errorf("concurrent bucket admission: accepted=%d refused=%d", accepted, refused)
	}
	for _, store := range stores {
		assertBucketCapacityMatchesFiles(t, store, ns)
	}
}
