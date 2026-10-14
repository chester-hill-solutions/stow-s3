package runtime_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

func TestDiskStagingAllowsWorkAndRechecksQuota(t *testing.T) {
	ctx := context.Background()
	store, err := fs.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendFilesystem, MaxBytes: 8, MaxObjects: 2}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	body := &waitingDiskBody{Reader: strings.NewReader("12345678"), entered: entered, release: release}
	staged := make(chan error, 1)
	go func() {
		_, err := instance.PutObjectStream(ctx, "assets", "staged", body, runtime.StreamPutOptions{Size: 8})
		staged <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("staging did not read")
	}
	head := make(chan error, 1)
	go func() { _, err := instance.HeadBucket(ctx, "assets"); head <- err }()
	select {
	case err := <-head:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("staging blocked metadata work")
	}
	if _, err := instance.PutObject(ctx, "assets", "extra", []byte("x"), runtime.PutOptions{}); !errors.Is(err, runtime.ErrQuotaExceeded) {
		t.Fatalf("in-flight bytes bypassed quota: %v", err)
	}
	once.Do(func() { close(release) })
	if err := <-staged; err != nil {
		t.Fatal(err)
	}
	if instance.Usage() != (runtime.Usage{Bytes: 8, Objects: 1}) {
		t.Fatalf("usage %+v", instance.Usage())
	}
}

type waitingDiskBody struct {
	io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *waitingDiskBody) Read(data []byte) (int, error) {
	r.once.Do(func() { close(r.entered); <-r.release })
	return r.Reader.Read(data)
}
