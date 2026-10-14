package runtime_test

import (
	"context"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestOwnedBodyStillEnforcesDeclaredStreamSize(t *testing.T) {
	ctx := context.Background()
	instance, err := runtime.Open(runtime.Options{MaxBytes: 16, MaxObjects: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int64{2, 4} {
		owned := storage.NewOwnedBody([]byte("abc"))
		if _, err := instance.PutObjectStream(ctx, "assets", "target", owned, runtime.StreamPutOptions{Size: size}); err == nil {
			t.Fatal("ownership token bypassed declared size")
		}
		if usage := instance.Usage(); usage.Bytes != 0 || usage.Objects != 0 {
			t.Fatalf("invalid ownership transfer changed usage: %+v", usage)
		}
		if _, err := instance.PutObjectStream(ctx, "assets", "target", owned, runtime.StreamPutOptions{Size: 3}); err != nil {
			t.Fatal(err)
		}
		if err := instance.DeleteObject(ctx, "assets", "target"); err != nil {
			t.Fatal(err)
		}
	}
}
