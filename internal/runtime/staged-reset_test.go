package runtime_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type resetStagedStore struct {
	storage.Store
	entered chan struct{}
	proceed chan struct{}
}

func (s *resetStagedStore) StageObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (storage.StagedObject, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	close(s.entered)
	<-s.proceed
	return &resetPendingObject{store: s.Store, bucket: bucket, key: key, data: data, opts: opts}, nil
}

type resetPendingObject struct {
	store       storage.Store
	bucket, key string
	data        []byte
	opts        storage.PutOptions
}

func (p *resetPendingObject) Commit(ctx context.Context) (*storage.ObjectMeta, error) {
	return p.store.PutObject(ctx, p.bucket, p.key, strings.NewReader(string(p.data)), p.opts)
}

func (p *resetPendingObject) Abort(context.Context) error { return nil }

func TestResetInvalidatesStagedPublicationWithoutLeakingReservations(t *testing.T) {
	ctx := context.Background()
	store := &resetStagedStore{Store: storage.NewMemoryStore(), entered: make(chan struct{}), proceed: make(chan struct{})}
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{MaxBytes: 6}, store, func() (storage.Store, error) { return storage.NewMemoryStore(), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	finished := make(chan error, 1)
	go func() {
		_, err := instance.PutObjectStream(ctx, "assets", "stale", strings.NewReader("123456"), runtime.StreamPutOptions{Size: 6})
		finished <- err
	}()
	<-store.entered
	if err := instance.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	close(store.proceed)
	if err := <-finished; err == nil {
		t.Fatal("old store publication succeeded after reset")
	}
	if usage := instance.Usage(); usage.Bytes != 0 || usage.Objects != 0 {
		t.Fatalf("stale usage: %+v", usage)
	}
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "assets", "fresh", []byte("123456"), runtime.PutOptions{}); err != nil {
		t.Fatalf("fresh quota: %v", err)
	}
	if usage := instance.Usage(); usage.Bytes != 6 || usage.Objects != 1 {
		t.Fatalf("fresh usage: %+v", usage)
	}
}
