package stow_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type stagedMemoryStore struct {
	*memoryStore
	entered chan struct{}
	aborts  int
}

func (s *stagedMemoryStore) StageObject(ctx context.Context, bucket, key string, body io.Reader, options stow.PutOptions) (stow.StagedObject, error) {
	if s.entered != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return &stagedMemoryObject{store: s, context: ctx, bucket: bucket, key: key, data: data, options: options}, nil
}

type stagedMemoryObject struct {
	store       *stagedMemoryStore
	context     context.Context
	bucket, key string
	data        []byte
	options     stow.PutOptions
	committed   bool
}

func (p *stagedMemoryObject) Commit(ctx context.Context) (stow.Object, error) {
	object, err := p.store.PutObject(ctx, p.bucket, p.key, p.data, p.options)
	p.committed = err == nil
	return object, err
}

func (p *stagedMemoryObject) Abort(context.Context) error {
	if !p.committed {
		p.store.aborts++
	}
	return nil
}

func openStagedRuntime(t *testing.T, maxBytes int64, initial string) (*stow.Runtime, *stagedMemoryStore) {
	t.Helper()
	ctx := context.Background()
	store := &stagedMemoryStore{memoryStore: newMemoryStore(), entered: make(chan struct{}, 1)}
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "assets", "target", []byte(initial), stow.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	runtime, err := stow.Open(stow.Options{Backend: stow.BackendHost, Store: store, MaxBytes: maxBytes})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime, store
}

func TestStagedWriteReservesQuotaWithoutBlockingReads(t *testing.T) {
	ctx := context.Background()
	runtime, store := openStagedRuntime(t, 8, "ab")

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	finished := make(chan error, 1)
	go func() {
		_, err := runtime.PutObjectStream(ctx, "assets", "stream", reader, stow.StreamPutOptions{Size: 6})
		finished <- err
	}()
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("staging did not start")
	}
	readDone := make(chan error, 1)
	go func() { _, err := runtime.HeadObject(ctx, "assets", "target"); readDone <- err }()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("staging blocks unrelated read")
	}
	if _, err := runtime.PutObject(ctx, "assets", "overflow", []byte("x"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("quota reservation: %v", err)
	}
	if _, err := writer.Write([]byte("123456")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if usage := runtime.Usage(); usage.Bytes != 8 || usage.Objects != 2 {
		t.Fatalf("usage: %+v", usage)
	}
}

func TestStreamDeclaredSizeMismatchLeavesPreviousObjectAndQuotaIntact(t *testing.T) {
	for _, data := range []string{"short", "too long"} {
		t.Run(data, func(t *testing.T) {
			ctx := context.Background()
			runtime, _ := openStagedRuntime(t, 10, "old")
			if _, err := runtime.PutObjectStream(ctx, "assets", "target", strings.NewReader(data), stow.StreamPutOptions{Size: 6}); err == nil {
				t.Fatal("declared size mismatch published")
			}
			object, err := runtime.GetObject(ctx, "assets", "target")
			if err != nil || string(object.Data) != "old" {
				t.Fatalf("previous object: %q %v", object.Data, err)
			}
			if usage := runtime.Usage(); usage.Bytes != 3 || usage.Objects != 1 {
				t.Fatalf("usage: %+v", usage)
			}
			if _, err := runtime.PutObject(ctx, "assets", "after", make([]byte, 7), stow.PutOptions{}); err != nil {
				t.Fatalf("reservation leaked: %v", err)
			}
		})
	}
}
