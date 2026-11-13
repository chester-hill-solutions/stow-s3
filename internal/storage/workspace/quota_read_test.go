package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestQuotaHeadDoesNotAdoptHostFile(t *testing.T) {
	store, err := New(Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	path := filepath.Join(store.Root(), "host.txt")
	if err := os.WriteFile(path, []byte("host data"), 0o600); err != nil {
		t.Fatal(err)
	}
	indexBefore, err := os.ReadFile(indexPath(store.Root()))
	if err != nil {
		t.Fatal(err)
	}
	quota := store.QuotaStore()
	meta, err := quota.HeadObject(context.Background(), "bucket", "host.txt")
	if err != nil || meta.Size != int64(len("host data")) {
		t.Fatalf("quota head = %+v, %v", meta, err)
	}
	if _, found := store.objectIndex.entry("bucket", "host.txt"); found {
		t.Fatal("quota head adopted host file")
	}
	indexAfter, _ := os.ReadFile(indexPath(store.Root()))
	if string(indexBefore) != string(indexAfter) {
		t.Fatal("quota head wrote index")
	}
}

func TestQuotaHeadReadsEscapedPrimaryAndSecondaryWithoutReconcile(t *testing.T) {
	store, err := New(Options{Root: t.TempDir(), Bucket: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "secondary"); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("x", 260)
	for _, bucket := range []string{"primary", "secondary"} {
		if _, err := store.PutObject(ctx, bucket, key, bytes.NewReader([]byte("before")), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		indexBefore, err := os.ReadFile(indexPath(store.Root()))
		if err != nil {
			t.Fatal(err)
		}
		path := EscapedPath(store.Root(), bucket, key)
		if err := os.WriteFile(path, []byte("after-size"), 0o600); err != nil {
			t.Fatal(err)
		}
		meta, err := store.QuotaStore().HeadObject(ctx, bucket, key)
		if err != nil || meta.Size != int64(len("after-size")) {
			t.Fatalf("quota head bucket=%s meta=%+v error=%v", bucket, meta, err)
		}
		indexAfter, err := os.ReadFile(indexPath(store.Root()))
		if err != nil || string(indexBefore) != string(indexAfter) {
			t.Fatalf("quota head reconciled escaped entry bucket=%s error=%v", bucket, err)
		}
	}
}
