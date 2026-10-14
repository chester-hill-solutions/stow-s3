package runtime_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type wholeReadStore struct {
	storage.Store
	entered chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (s *wholeReadStore) GetObject(context.Context, string, string) (io.ReadCloser, *storage.ObjectMeta, error) {
	return &wholeReader{store: s}, &storage.ObjectMeta{Size: 1}, nil
}
func (s *wholeReadStore) Close() error { close(s.closed); return s.Store.Close() }

type wholeReader struct{ store *wholeReadStore }

func (r *wholeReader) Read(data []byte) (int, error) {
	r.store.entered <- struct{}{}
	<-r.store.release
	data[0] = 'x'
	return 1, io.EOF
}
func (*wholeReader) Close() error { return nil }

func TestWholeObjectReadsOverlapAndCloseWaits(t *testing.T) {
	store := &wholeReadStore{Store: storage.NewMemoryStore(), entered: make(chan struct{}, 2), release: make(chan struct{}), closed: make(chan struct{})}
	instance, err := runtime.OpenWithStore(runtime.Options{}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	defer instance.Close()
	defer release.Do(func() { close(store.release) })
	done := make(chan error, 2)
	for range 2 {
		go readWholeObject(instance, done)
	}
	for range 2 {
		select {
		case <-store.entered:
		case <-time.After(5 * time.Second):
			release.Do(func() { close(store.release) })
			<-done
			<-done
			t.Fatal("whole-object reads serialized")
		}
	}
	closing := make(chan struct{})
	go func() { _ = instance.Close(); close(closing) }()
	select {
	case <-store.closed:
		t.Fatal("store closed during reads")
	case <-time.After(20 * time.Millisecond):
	}
	release.Do(func() { close(store.release) })
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-closing:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not finish")
	}
}

func readWholeObject(instance *runtime.Instance, done chan<- error) {
	got, err := instance.GetObject(context.Background(), "assets", "key")
	if err == nil && string(got.Data) != "x" {
		err = io.ErrUnexpectedEOF
	}
	done <- err
}
