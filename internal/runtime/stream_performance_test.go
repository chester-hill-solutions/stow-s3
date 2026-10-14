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
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestAdapterReturnsStreamBeforeReadingPayload(t *testing.T) {
	ctx := context.Background()
	store := &observedStreamStore{Store: storage.NewMemoryStore()}
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "assets", "key", strings.NewReader("payload"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	adapter, err := runtime.NewStoreAdapter(instance)
	if err != nil {
		t.Fatal(err)
	}
	reader, meta, err := adapter.GetObject(ctx, "assets", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if meta.Size != 7 || store.reads != 0 {
		t.Fatalf("premature reads=%d metadata=%+v", store.reads, meta)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, runtime.ErrClosed) {
		t.Fatalf("read after runtime close: %v", err)
	}
	if store.reads != 0 || store.closes != 1 {
		t.Fatalf("reads=%d closes=%d", store.reads, store.closes)
	}
}

func TestAdapterSeekUsesCapturedVersion(t *testing.T) {
	ctx := context.Background()
	instance, err := runtime.Open(runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "assets", "key", []byte("old-payload"), runtime.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	adapter, err := runtime.NewStoreAdapter(instance)
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := adapter.GetObject(ctx, "assets", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := instance.PutObject(ctx, "assets", "key", []byte("new-payload"), runtime.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	seeker, ok := reader.(io.ReadSeeker)
	if !ok {
		t.Fatal("adapter erased seek support")
	}
	if _, err := seeker.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "payload" {
		t.Fatalf("captured range=%q error=%v", body, err)
	}
}

type parallelReadStore struct {
	storage.Store
	entered chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (s *parallelReadStore) GetObject(context.Context, string, string) (io.ReadCloser, *storage.ObjectMeta, error) {
	return &blockedObjectReader{store: s}, &storage.ObjectMeta{Size: 1}, nil
}
func (s *parallelReadStore) Close() error { close(s.closed); return s.Store.Close() }

type blockedObjectReader struct{ store *parallelReadStore }

func (r *blockedObjectReader) Read(buffer []byte) (int, error) {
	r.store.entered <- struct{}{}
	<-r.store.release
	buffer[0] = 'x'
	return 1, nil
}
func (*blockedObjectReader) Close() error { return nil }

func parallelStreams(t *testing.T) (*runtime.Instance, *parallelReadStore, []io.ReadCloser) {
	t.Helper()
	store := &parallelReadStore{Store: storage.NewMemoryStore(), entered: make(chan struct{}, 2), release: make(chan struct{}), closed: make(chan struct{})}
	instance, err := runtime.OpenWithStore(runtime.Options{}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	var readers []io.ReadCloser
	for range 2 {
		reader, _, err := instance.OpenObject(context.Background(), "assets", "key")
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, reader)
	}
	return instance, store, readers
}

func TestIndependentStreamsReadConcurrentlyAndCloseWaits(t *testing.T) {
	instance, store, readers := parallelStreams(t)
	var release sync.Once
	defer instance.Close()
	defer release.Do(func() { close(store.release) })
	var workers sync.WaitGroup
	for _, reader := range readers {
		workers.Add(1)
		go func() { defer workers.Done(); _, _ = reader.Read(make([]byte, 1)) }()
	}
	for range 2 {
		select {
		case <-store.entered:
		case <-time.After(5 * time.Second):
			release.Do(func() { close(store.release) })
			workers.Wait()
			t.Fatal("independent reader blocked behind runtime mutex")
		}
	}
	closing := make(chan struct{})
	go func() { _ = instance.Close(); close(closing) }()
	select {
	case <-store.closed:
		t.Fatal("store closed while reads were active")
	case <-time.After(20 * time.Millisecond):
	}
	release.Do(func() { close(store.release) })
	workers.Wait()
	select {
	case <-closing:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not complete after reads")
	}
	for _, reader := range readers {
		_ = reader.Close()
	}
}
