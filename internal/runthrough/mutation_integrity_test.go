package runthrough_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestRegressionCopyPreservesConcurrentUpstreamUpdate(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := local.PutObject(ctx, "bucket", "source", bytes.NewBufferString("agent copy"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	up := conflictUpstream(t, ctx, "original")
	adapter := mirroringAdapter(t, local, up)
	readThrough(t, adapter, ctx)
	if _, err := up.PutObject(ctx, "bucket", "key", bytes.NewBufferString("device update"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.CopyObject(ctx, "bucket", "source", "bucket", "key")
	if !errors.Is(err, runthrough.ErrUpstreamConflict) {
		t.Fatalf("copy silently overwrote another writer: err=%v upstream=%q", err, up.bodies[objectKey("bucket", "key")])
	}
}

func TestRegressionRepeatedEditKeepsProvenance(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	up := conflictUpstream(t, ctx, "original")
	adapter := mirroringAdapter(t, local, up)
	readThrough(t, adapter, ctx)
	if _, err := adapter.PutObject(ctx, "bucket", "key", bytes.NewBufferString("agent first"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	readThrough(t, adapter, ctx)
	if _, err := up.PutObject(ctx, "bucket", "key", bytes.NewBufferString("device update"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.PutObject(ctx, "bucket", "key", bytes.NewBufferString("agent second"), storage.PutOptions{})
	if !errors.Is(err, runthrough.ErrUpstreamConflict) {
		t.Fatalf("second edit silently overwrote another writer: err=%v upstream=%q", err, up.bodies[objectKey("bucket", "key")])
	}
}

func TestRegressionPropagationFailureStillAccountsLocalStorage(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	up := newMockUpstream()
	adapter := mirroringAdapter(t, local, up)
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendMemory, MaxObjects: 1, MaxBytes: 100}, adapter, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	up.putErr = runthrough.NewTransientUpstreamError(errors.New("upstream unavailable"))
	if _, err := instance.PutObject(ctx, "bucket", "first", []byte("kept locally"), runtime.PutOptions{}); err == nil {
		t.Fatal("expected propagation error")
	}
	if _, err := local.HeadObject(ctx, "bucket", "first"); err != nil {
		t.Fatal(err)
	}
	usage := instance.Usage()
	if usage.Objects != 1 || usage.Bytes != 12 {
		t.Errorf("committed local object missing from quota accounting: usage=%+v", usage)
	}
	_, err = instance.PutObject(ctx, "bucket", "second", []byte("another local object"), runtime.PutOptions{})
	if !errors.Is(err, runtime.ErrQuotaExceeded) {
		_, headErr := local.HeadObject(ctx, "bucket", "second")
		t.Fatalf("MaxObjects=1 bypassed: second write err=%v second local object err=%v", err, headErr)
	}
}
