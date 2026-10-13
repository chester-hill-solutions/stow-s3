package stow_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type lazyStore struct {
	*memoryStore
	opens  int
	reads  int
	closes int
}

func (s *lazyStore) GetObject(context.Context, string, string) (stow.Object, error) {
	return stow.Object{}, errors.New("whole-object reads must not be used")
}

func (s *lazyStore) OpenObject(ctx context.Context, bucket, key string) (io.ReadCloser, stow.Object, error) {
	object, err := s.HeadObject(ctx, bucket, key)
	if err != nil {
		return nil, stow.Object{}, err
	}
	s.opens++
	reader := &observedReader{Reader: bytes.NewReader(object.Data), store: s}
	object.Data = nil
	return reader, object, nil
}

type observedReader struct {
	*bytes.Reader
	store  *lazyStore
	closed bool
}

func (r *observedReader) Read(buffer []byte) (int, error) {
	r.store.reads++
	return r.Reader.Read(buffer)
}

func (r *observedReader) Close() error {
	if !r.closed {
		r.closed = true
		r.store.closes++
	}
	return nil
}

func openLazyRuntime(t *testing.T) (*stow.Runtime, *lazyStore) {
	t.Helper()
	store := &lazyStore{memoryStore: newMemoryStore()}
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "assets", "document", []byte("0123456789"), stow.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	runtime, err := stow.Open(stow.Options{Backend: stow.BackendHost, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime, store
}

func TestOpenObjectUsesMetadataOnlyInitializationAndLazyReads(t *testing.T) {
	runtime, store := openLazyRuntime(t)
	if store.opens != 0 || store.reads != 0 {
		t.Fatal("initialization read payloads")
	}
	reader, object, err := runtime.OpenObject(context.Background(), "assets", "document")
	if err != nil {
		t.Fatal(err)
	}
	if object.Data != nil || object.Size != 10 || store.reads != 0 {
		t.Fatal("open materialized payload")
	}
	buffer := make([]byte, 2)
	if n, err := reader.Read(buffer); n != 2 || err != nil || string(buffer) != "01" {
		t.Fatalf("read: %d %q %v", n, buffer, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil || store.closes != 1 {
		t.Fatalf("close: %d %v", store.closes, err)
	}
}

func TestObjectStreamsHonorCancellationAndClosedRuntime(t *testing.T) {
	for _, terminate := range []string{"cancel", "close"} {
		t.Run(terminate, func(t *testing.T) {
			runtime, store := openLazyRuntime(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader, _, err := runtime.OpenObject(ctx, "assets", "document")
			if err != nil {
				t.Fatal(err)
			}
			want := context.Canceled
			if terminate == "cancel" {
				cancel()
			} else {
				_ = runtime.Close()
				want = stow.ErrClosed
			}
			if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, want) {
				t.Fatalf("read error: %v", err)
			}
			if store.reads != 0 || store.closes != 1 {
				t.Fatalf("payload reads=%d closes=%d", store.reads, store.closes)
			}
		})
	}
}

func TestObjectRangeReturnsRequestedBytesAndWholeObjectMetadata(t *testing.T) {
	runtime, store := openLazyRuntime(t)
	reader, object, err := runtime.OpenObjectRange(context.Background(), "assets", "document", stow.ObjectRange{Offset: 3, Length: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "3456" || object.Size != 10 {
		t.Fatalf("range %q size=%d error=%v", data, object.Size, err)
	}
	if store.closes != 1 {
		t.Fatal("range did not close its captured reader")
	}
	for _, span := range []stow.ObjectRange{{Offset: -1, Length: 1}, {Offset: 9, Length: 2}} {
		if _, _, err := runtime.OpenObjectRange(context.Background(), "assets", "document", span); err == nil {
			t.Fatalf("invalid range accepted: %+v", span)
		}
	}
}
