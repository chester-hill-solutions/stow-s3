package fs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func capacityScanFixture(t testing.TB, root string, objects int) (*FilesystemStore, *capacity.Namespace) {
	t.Helper()
	if !(&FilesystemStore{}).SupportsGuardedWrites() {
		t.Skip("requires native namespace admission")
	}
	var dir string
	if root == "" {
		dir = t.TempDir()
	} else {
		var err error
		dir, err = os.MkdirTemp(root, "scan-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	store, err := NewFilesystemStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if objects != 0 {
		body := bytes.Repeat([]byte("x"), 1<<20)
		for i := 0; i < objects; i++ {
			if _, err := store.PutObject(ctx, "bucket", fmt.Sprintf("existing-%d", i), bytes.NewReader(body), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	host, err := capacity.Open(capacity.HostOptions{Dir: filepath.Join(dir, "host"), MaxBytes: 128 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ns, err := host.Bind(capacity.NamespaceOptions{ID: "scan", MaxBytes: 64 << 20, RecoveryReserveBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BindNamespace(ns); err != nil {
		t.Fatal(err)
	}
	return store, ns
}

func TestCapacityMutationAllocation(t *testing.T) {
	for _, op := range []string{"put", "save", "delete"} {
		t.Run(op, func(t *testing.T) {
			store, ns := capacityScanFixture(t, "", 8)
			body := bytes.Repeat([]byte("y"), 64<<10)
			ctx := context.Background()
			if op == "delete" {
				if err := store.CreateBucket(ctx, "empty"); err != nil {
					t.Fatal(err)
				}
			}
			opts := storage.PutOptions{}
			if op == "save" {
				opts.RequestKey = "request"
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			var err error
			if op == "delete" {
				err = store.DeleteBucket(ctx, "empty")
			} else {
				_, err = store.PutObject(ctx, "bucket", "target", bytes.NewReader(body), opts)
			}
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("%s with 8 MiB of existing payload: %d allocated bytes/op", op, allocated)
			// Leave room for one full verified read and metadata. A second payload
			// read makes small mutations allocate more than the 12 MiB regression budget.
			if allocated > 12<<20 {
				t.Errorf("%s repeated existing payload reads: %d allocated bytes/op, budget=%d", op, allocated, 12<<20)
			}
			assertBucketCapacityMatchesFiles(t, store, ns)
		})
	}
}
