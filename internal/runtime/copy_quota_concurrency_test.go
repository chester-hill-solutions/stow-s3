package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

// Prepare a larger source while COPY is between source HEAD and backend COPY.
// The staging work may finish there, but publication must serialize with COPY.
func TestCopyQuotaWhileSourceOverwriteIsStaged(t *testing.T) {
	for _, backend := range []runtime.Backend{runtime.BackendMemory, runtime.BackendFilesystem} {
		for _, conditioned := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/conditioned=%t", backend, conditioned), func(t *testing.T) {
				checkCopyQuotaDuringSourceStaging(t, backend, conditioned)
			})
		}
	}
}

func checkCopyQuotaDuringSourceStaging(t *testing.T, backend runtime.Backend, conditioned bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hooks := &copyStagingHooks{prepared: make(chan struct{})}
	instance, source := copyStagingInstance(t, ctx, backend, hooks)

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	body := &waitingDiskBody{Reader: strings.NewReader("1234567"), entered: entered, release: release}
	written := make(chan error, 1)
	go func() {
		_, err := instance.PutObjectStream(ctx, "assets", "source", body, runtime.StreamPutOptions{Size: 7})
		written <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("overwrite never started staging")
	}
	// This is the only intervention: let the real backend finish preparing the
	// overwrite after HEAD has observed the small source. All publications still
	// go through the runtime; no unsupported direct store writer is introduced.
	hooks.afterHead = func() {
		once.Do(func() { close(release) })
		select {
		case <-hooks.prepared:
		case <-ctx.Done():
			t.Error("overwrite did not finish preparing during COPY")
		}
	}
	req := storage.CopyRequest{SourceBucket: "assets", SourceKey: "source", DestBucket: "assets", DestKey: "copy"}
	if conditioned {
		req.Options.SourceIfMatch = source.ETag
	}
	copied, err := instance.CopyObjectCond(ctx, req)
	if err != nil || copied.Size != 1 || copied.ETag != source.ETag {
		t.Fatalf("copy = %+v, error = %v; want admitted one-byte source", copied, err)
	}
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("overwrite did not publish after COPY")
	}
	assertStagedCopyQuota(t, ctx, instance)
}

func assertStagedCopyQuota(t *testing.T, ctx context.Context, instance *runtime.Instance) {
	t.Helper()
	for key, want := range map[string]string{"source": "1234567", "copy": "x"} {
		object, err := instance.GetObject(ctx, "assets", key)
		if err != nil || string(object.Data) != want {
			t.Fatalf("%s = %q, error = %v; want %q", key, object.Data, err, want)
		}
	}
	if usage := instance.Usage(); usage != (runtime.Usage{Bytes: 8, Objects: 2}) {
		t.Fatalf("usage = %+v, want exact eight-byte cap", usage)
	}
	if _, err := instance.CopyObject(ctx, "assets", "source", "assets", "copy"); !errors.Is(err, runtime.ErrQuotaExceeded) {
		t.Fatalf("larger published source bypassed quota: %v", err)
	}
	object, err := instance.GetObject(ctx, "assets", "copy")
	if err != nil || string(object.Data) != "x" {
		t.Fatalf("refused copy changed destination: %+v, %v", object, err)
	}
}

func copyStagingInstance(t *testing.T, ctx context.Context, backend runtime.Backend, hooks *copyStagingHooks) (*runtime.Instance, runtime.Object) {
	t.Helper()
	var store storage.Store
	if backend == runtime.BackendMemory {
		store = &copyStagingMemory{MemoryStore: storage.NewMemoryStore(), hooks: hooks}
	} else {
		disk, err := fs.NewFilesystemStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		store = &copyStagingFilesystem{FilesystemStore: disk, hooks: hooks}
	}
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: backend, MaxBytes: 8, MaxObjects: 2}, store, nil)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	source, err := instance.PutObject(ctx, "assets", "source", []byte("x"), runtime.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return instance, source
}

type copyStagingHooks struct {
	afterHead func()
	prepared  chan struct{}
}

func (h *copyStagingHooks) head(meta *storage.ObjectMeta, err error) (*storage.ObjectMeta, error) {
	if err == nil && meta.Key == "source" && h.afterHead != nil {
		h.afterHead()
	}
	return meta, err
}

func (h *copyStagingHooks) staged(pending storage.StagedObject, err error) (storage.StagedObject, error) {
	if h.afterHead != nil {
		close(h.prepared)
	}
	return pending, err
}

type copyStagingMemory struct {
	*storage.MemoryStore
	hooks *copyStagingHooks
}

func (s *copyStagingMemory) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	return s.hooks.head(s.MemoryStore.HeadObject(ctx, bucket, key))
}

func (s *copyStagingMemory) StageObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (storage.StagedObject, error) {
	return s.hooks.staged(s.MemoryStore.StageObject(ctx, bucket, key, body, opts))
}

type copyStagingFilesystem struct {
	*fs.FilesystemStore
	hooks *copyStagingHooks
}

func (s *copyStagingFilesystem) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	return s.hooks.head(s.FilesystemStore.HeadObject(ctx, bucket, key))
}

func (s *copyStagingFilesystem) StageObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (storage.StagedObject, error) {
	return s.hooks.staged(s.FilesystemStore.StageObject(ctx, bucket, key, body, opts))
}
