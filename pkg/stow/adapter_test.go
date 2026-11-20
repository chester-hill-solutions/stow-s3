package stow_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence/directory"
)

func TestOpenAdapterServesObjectsThroughTheEmbeddedRuntime(t *testing.T) {
	ctx, runtime := openAdapterRuntime(t)
	assertConditionalCreate(t, ctx, runtime)
	assertAdapterReads(t, ctx, runtime)
	assertAdapterDelete(t, ctx, runtime)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func openAdapterRuntime(t *testing.T) (context.Context, *stow.Runtime) {
	t.Helper()
	adapter, err := directory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := stow.OpenAdapter(stow.Options{Backend: stow.BackendFilesystem, MaxBytes: 1 << 20, MaxObjects: 100}, adapter, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatal(err)
	}
	return context.Background(), runtime
}

func assertConditionalCreate(t *testing.T, ctx context.Context, runtime *stow.Runtime) {
	t.Helper()
	if _, err := runtime.PutObject(ctx, "bucket", "report.txt", []byte("first"),
		stow.PutOptions{ContentType: "text/plain", IfNoneMatch: "*"}); err != nil {
		t.Fatalf("create-if-absent through the adapter: %v", err)
	}
	if _, err := runtime.PutObject(ctx, "bucket", "report.txt", []byte("second"),
		stow.PutOptions{IfNoneMatch: "*"}); !errors.Is(err, stow.ErrPreconditionFailed) {
		t.Fatalf("second create-if-absent = %v", err)
	}
}

func assertAdapterReads(t *testing.T, ctx context.Context, runtime *stow.Runtime) {
	t.Helper()
	object, err := runtime.GetObject(ctx, "bucket", "report.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(object.Data, []byte("first")) || object.ContentType != "text/plain" || object.ETag == "" {
		t.Fatalf("object = %+v", object)
	}
	head, err := runtime.HeadObject(ctx, "bucket", "report.txt")
	if err != nil || head.ETag != object.ETag || head.Size != object.Size {
		t.Fatalf("head = %+v, %v", head, err)
	}
	page, err := runtime.ListObjects(ctx, "bucket", stow.ListOptions{Limit: 10})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Key != "report.txt" {
		t.Fatalf("list = %+v, %v", page, err)
	}
	buckets, err := runtime.ListBuckets(ctx)
	if err != nil || len(buckets) != 1 || buckets[0].Name != "bucket" {
		t.Fatalf("buckets = %+v, %v", buckets, err)
	}
}

func assertAdapterDelete(t *testing.T, ctx context.Context, runtime *stow.Runtime) {
	t.Helper()
	if err := runtime.DeleteObject(ctx, "bucket", "report.txt"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DeleteObject(ctx, "bucket", "report.txt"); err != nil {
		t.Fatalf("deleting an absent key = %v", err)
	}
}

func TestOpenAdapterKeepsBytesAcrossReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	adapter, err := directory.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := stow.OpenAdapter(stow.Options{Backend: stow.BackendFilesystem}, adapter, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.PutObject(ctx, "bucket", "kept.txt", []byte("durable"), stow.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := directory.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := stow.OpenAdapter(stow.Options{Backend: stow.BackendFilesystem}, reopened, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	object, err := second.GetObject(ctx, "bucket", "kept.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(object.Data) != "durable" {
		t.Fatalf("reopened bytes = %q", object.Data)
	}
}

type stubStore struct{}

func (stubStore) CreateBucket(context.Context, string) error         { return nil }
func (stubStore) DeleteBucket(context.Context, string) error         { return nil }
func (stubStore) ListBuckets(context.Context) ([]stow.Bucket, error) { return nil, nil }
func (stubStore) PutObject(context.Context, string, string, []byte, stow.PutOptions) (stow.Object, error) {
	return stow.Object{}, nil
}
func (stubStore) GetObject(context.Context, string, string) (stow.Object, error) {
	return stow.Object{}, nil
}
func (stubStore) HeadObject(context.Context, string, string) (stow.Object, error) {
	return stow.Object{}, nil
}
func (stubStore) DeleteObject(context.Context, string, string) error { return nil }
func (stubStore) ListObjects(context.Context, string, stow.ListOptions) (stow.ObjectPage, error) {
	return stow.ObjectPage{}, nil
}
func (stubStore) Close() error { return nil }

func TestOpenAdapterRefusesTwoStores(t *testing.T) {
	adapter, err := directory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	if _, err := stow.OpenAdapter(stow.Options{Store: stubStore{}}, adapter, "bucket"); err == nil {
		t.Fatal("two stores were accepted")
	}
}
