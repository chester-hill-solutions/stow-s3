package runtime_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type waitingMemoryReader struct {
	io.Reader
	entered chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (r *waitingMemoryReader) Read(data []byte) (int, error) {
	r.once.Do(func() { close(r.entered); <-r.proceed })
	return r.Reader.Read(data)
}

func TestMemoryStreamPreparationDoesNotBlockReads(t *testing.T) {
	ctx := context.Background()
	instance, err := runtime.Open(runtime.Options{MaxBytes: 6, MaxObjects: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "assets", "existing", []byte("ab"), runtime.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	reader := &waitingMemoryReader{Reader: strings.NewReader("1234"), entered: make(chan struct{}), proceed: make(chan struct{})}
	finished := make(chan error, 1)
	go func() {
		_, err := instance.PutObjectStream(ctx, "assets", "pending", reader, runtime.StreamPutOptions{Size: 4})
		finished <- err
	}()
	select {
	case <-reader.entered:
	case <-time.After(time.Second):
		t.Fatal("body preparation never started")
	}
	readDone := make(chan error, 1)
	go func() { _, err := instance.HeadObject(ctx, "assets", "existing"); readDone <- err }()
	unblocked := memoryReadReady(t, readDone)
	if unblocked {
		if _, err := instance.PutObject(ctx, "assets", "overflow", []byte("x"), runtime.PutOptions{}); !errors.Is(err, runtime.ErrQuotaExceeded) {
			t.Errorf("active write reservation lost: %v", err)
		}
	}
	close(reader.proceed)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if !unblocked {
		<-readDone
	}
	if usage := instance.Usage(); usage.Bytes != 6 || usage.Objects != 2 {
		t.Fatalf("committed usage: %+v", usage)
	}
}

func memoryReadReady(t *testing.T, readDone <-chan error) bool {
	t.Helper()
	select {
	case err := <-readDone:
		if err != nil {
			t.Error(err)
		}
		return true
	case <-time.After(500 * time.Millisecond):
		t.Error("memory body preparation holds the runtime lock")
		return false
	}
}

type waitingBufferedStore struct {
	storage.Store
	entered chan struct{}
	proceed chan struct{}
}

func (s *waitingBufferedStore) StageObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (storage.StagedObject, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	close(s.entered)
	<-s.proceed
	return &resetPendingObject{store: s.Store, bucket: bucket, key: key, data: data, opts: opts}, nil
}

func TestBufferedPutStagesAndRejectsReset(t *testing.T) {
	ctx := context.Background()
	store := &waitingBufferedStore{Store: storage.NewMemoryStore(), entered: make(chan struct{}), proceed: make(chan struct{})}
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{MaxBytes: 4}, store, func() (storage.Store, error) { return storage.NewMemoryStore(), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	finished := make(chan error, 1)
	go func() {
		_, err := instance.PutObject(ctx, "assets", "stale", []byte("1234"), runtime.PutOptions{})
		finished <- err
	}()
	select {
	case <-store.entered:
	case err := <-finished:
		t.Fatalf("buffered PUT bypassed staging: %v", err)
	case <-time.After(time.Second):
		t.Fatal("buffered staging never started")
	}
	if err := instance.Reset(ctx); err != nil {
		close(store.proceed)
		<-finished
		t.Fatal(err)
	}
	close(store.proceed)
	if err := <-finished; err == nil {
		t.Fatal("old buffered write published after reset")
	}
	if usage := instance.Usage(); usage.Bytes != 0 || usage.Objects != 0 {
		t.Fatalf("stale reservation after reset: %+v", usage)
	}
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "assets", "fresh", []byte("1234"), runtime.PutOptions{}); err != nil {
		t.Fatalf("quota leaked after reset: %v", err)
	}
}

func TestMemoryStagedWriteRechecksBeforeCommit(t *testing.T) {
	for _, change := range []string{"cancel", "close", "reset", "replace", "delete-bucket", "revoke"} {
		t.Run(change, func(t *testing.T) {
			paused := pauseMemoryWrite(t)
			expected := changePausedMemoryWrite(t, paused, change)
			paused.unblock()
			assertMemoryWriteRefused(t, paused, expected)
			assertMemoryReservationReleased(t, paused, change)
		})
	}
}

type pausedMemoryWrite struct {
	instance *runtime.Instance
	cancel   context.CancelFunc
	current  atomic.Pointer[policy.Set]
	allowed  policy.Set
	finished chan error
	unblock  func()
}

func pauseMemoryWrite(t *testing.T) *pausedMemoryWrite {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &pausedMemoryWrite{cancel: cancel, finished: make(chan error, 1)}
	allowPrefix(&p.allowed, "assets", "", authority.ObjectWrite, authority.ObjectRead, authority.ObjectDelete)
	p.current.Store(&p.allowed)
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{MaxBytes: 8, MaxObjects: 2, Policy: func() (policy.Set, error) { return *p.current.Load(), nil }}, store, func() (storage.Store, error) { return storage.NewMemoryStore(), nil })
	if err != nil {
		t.Fatal(err)
	}
	p.instance = instance
	t.Cleanup(func() { instance.Close() })
	original, err := instance.PutObject(ctx, "assets", "target", []byte("old"), runtime.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reader := &waitingMemoryReader{Reader: strings.NewReader("next"), entered: make(chan struct{}), proceed: make(chan struct{})}
	var release sync.Once
	p.unblock = func() { release.Do(func() { close(reader.proceed) }) }
	t.Cleanup(p.unblock)
	go func() {
		_, err := instance.PutObjectStream(ctx, "assets", "target", reader, runtime.StreamPutOptions{Size: 4, PutOptions: runtime.PutOptions{IfMatch: original.ETag}})
		p.finished <- err
	}()
	select {
	case <-reader.entered:
	case <-time.After(time.Second):
		t.Fatal("preparation never started")
	}
	return p
}

func changePausedMemoryWrite(t *testing.T, p *pausedMemoryWrite, change string) error {
	t.Helper()
	ctx := context.Background()
	var err, expected error
	switch change {
	case "cancel":
		p.cancel()
		expected = context.Canceled
	case "close":
		err = p.instance.Close()
		expected = runtime.ErrClosed
	case "reset":
		err = p.instance.Reset(ctx)
	case "replace":
		_, err = p.instance.PutObject(ctx, "assets", "target", []byte("new"), runtime.PutOptions{})
		expected = storage.ErrPreconditionFailed
	case "delete-bucket":
		err = p.instance.DeleteObject(ctx, "assets", "target")
		if err == nil {
			err = p.instance.DeleteBucket(ctx, "assets")
		}
		expected = storage.ErrBucketNotFound
	case "revoke":
		p.current.Store(&policy.Set{})
	default:
		t.Fatal("unknown staged change")
	}
	if err != nil {
		t.Fatal(err)
	}
	return expected
}

func assertMemoryWriteRefused(t *testing.T, p *pausedMemoryWrite, expected error) {
	t.Helper()
	select {
	case err := <-p.finished:
		if err == nil || (expected != nil && !errors.Is(err, expected)) {
			t.Fatalf("staged publication: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("staged write did not finish")
	}
	if usage := p.instance.Usage(); usage.Bytes > 3 {
		t.Fatalf("refused write changed usage: %+v", usage)
	}
}

func assertMemoryReservationReleased(t *testing.T, p *pausedMemoryWrite, change string) {
	t.Helper()
	if change == "revoke" {
		p.current.Store(&p.allowed)
	}
	switch change {
	case "replace", "cancel", "revoke":
		if _, err := p.instance.PutObject(context.Background(), "assets", "fresh", []byte("12345"), runtime.PutOptions{}); err != nil {
			t.Fatalf("refused write leaked quota: %v", err)
		}
	}
}
