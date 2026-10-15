package workspace

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The workspace store shares one object index between its write and read paths,
// so concurrent readers and writers must not touch it without the lock. This
// test exists because the ordinary suite is sequential: every method looked
// correct on its own, and the store's RWMutex was only ever taken for writing.
// Under `-race` this is the test that fails when a read path forgets to lock.
//
// The workspace backend is the one the runtime's own mutex does not cover, since
// the embedded API hands out Path and the store directly.
func TestConcurrentReadsAndWritesAreSafe(t *testing.T) {
	store, err := New(Options{Root: t.TempDir(), Bucket: "wsbucket"})
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	const iterations = 500
	var wg sync.WaitGroup
	run := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				fn(i)
			}
		}()
	}

	run(func(i int) {
		if _, err := store.PutObject(ctx, "wsbucket", fmt.Sprintf("k%03d", i%16), bytes.NewReader([]byte("x")), storage.PutOptions{}); err != nil {
			t.Errorf("put: %v", err)
		}
	})
	run(func(int) {
		if _, err := store.ListObjectsV2(ctx, "wsbucket", storage.ListOptions{}); err != nil {
			t.Errorf("list: %v", err)
		}
	})
	run(func(i int) {
		store.Path("wsbucket", fmt.Sprintf("k%03d", i%16))
	})
	run(func(i int) {
		_ = store.DeleteObject(ctx, "wsbucket", fmt.Sprintf("k%03d", i%16))
	})

	wg.Wait()
}
