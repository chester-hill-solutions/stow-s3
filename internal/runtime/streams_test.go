package runtime_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type observedStreamStore struct {
	storage.Store
	reads  int
	closes int
}

func (s *observedStreamStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	reader, meta, err := s.Store.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	return &policyReader{ReadCloser: reader, store: s}, meta, nil
}

type policyReader struct {
	io.ReadCloser
	store *observedStreamStore
}

func (r *policyReader) Read(buffer []byte) (int, error) {
	r.store.reads++
	return r.ReadCloser.Read(buffer)
}

func (r *policyReader) Close() error {
	r.store.closes++
	return r.ReadCloser.Close()
}

func TestStreamRechecksPolicyBeforeReadingCapturedVersion(t *testing.T) {
	ctx := context.Background()
	store := &observedStreamStore{Store: storage.NewMemoryStore()}
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "assets", "document", strings.NewReader("secret"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	var allowed policy.Set
	allowPrefix(&allowed, "assets", "", authority.ObjectRead)
	current := &allowed
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendHost, Policy: func() (policy.Set, error) { return *current, nil }}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	reader, _, err := instance.OpenObject(ctx, "assets", "document")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	current = &policy.Set{}
	if _, err := reader.Read(make([]byte, 6)); !denied(t, err) {
		t.Fatalf("revoked stream read: %v", err)
	}
	if store.reads != 0 || store.closes != 1 {
		t.Fatalf("unauthorized payload reads=%d closes=%d", store.reads, store.closes)
	}
}

type revokingStreamWriter struct {
	current **policy.Set
}

func (w revokingStreamWriter) Write(data []byte) (int, error) {
	*w.current = &policy.Set{}
	return len(data), nil
}

func TestStreamWriteToRechecksPolicyBetweenChunks(t *testing.T) {
	ctx := context.Background()
	store := &observedStreamStore{Store: storage.NewMemoryStore()}
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("x", 256<<10)
	if _, err := store.PutObject(ctx, "assets", "document", strings.NewReader(body), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	var allowed policy.Set
	allowPrefix(&allowed, "assets", "", authority.ObjectRead)
	current := &allowed
	instance, err := runtime.OpenWithStore(runtime.Options{Policy: func() (policy.Set, error) { return *current, nil }}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	reader, _, err := instance.OpenObject(ctx, "assets", "document")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	written, err := io.Copy(revokingStreamWriter{current: &current}, reader)
	if !denied(t, err) || written <= 0 || written >= int64(len(body)) {
		t.Fatalf("revoked transfer wrote=%d err=%v", written, err)
	}
	if store.reads != 1 || store.closes != 1 {
		t.Fatalf("reads=%d closes=%d", store.reads, store.closes)
	}
}
