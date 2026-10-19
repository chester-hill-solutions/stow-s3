package embedded

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type embeddedSaveBodyStore struct {
	*storage.MemoryStore
	bodyErr       error
	together      bool
	bytes, closes int
}

func (s *embeddedSaveBodyStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	reader, meta, err := s.MemoryStore.GetObject(ctx, bucket, key)
	if err != nil || s.bodyErr == nil {
		return reader, meta, err
	}
	return &embeddedSaveBodyReader{ReadCloser: reader, store: s}, meta, nil
}

type embeddedSaveBodyReader struct {
	io.ReadCloser
	store *embeddedSaveBodyStore
}

func (r *embeddedSaveBodyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.store.bytes > 0 {
		return 0, r.store.bodyErr
	}
	n, err := r.ReadCloser.Read(p[:1])
	r.store.bytes += n
	if r.store.together {
		return n, r.store.bodyErr
	}
	return n, err
}
func (r *embeddedSaveBodyReader) Close() error { r.store.closes++; return r.ReadCloser.Close() }

func TestEmbeddedReadForSaveBodyMissing(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		for _, together := range []bool{false, true} {
			t.Run(fmt.Sprintf("wrapped=%t/together=%t", wrapped, together), func(t *testing.T) { checkEmbeddedSaveBody(t, wrapped, together) })
		}
	}
}

func embeddedSaveBodyFixture(t *testing.T) (*embeddedSaveBodyStore, *Runtime) {
	t.Helper()
	ctx := context.Background()
	store := &embeddedSaveBodyStore{MemoryStore: storage.NewMemoryStore()}
	inner, err := stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendMemory, MaxBytes: 8, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	outer := &Runtime{inner: inner}
	t.Cleanup(func() { _ = outer.Close() })
	if err := outer.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	if _, err := outer.PutObject(ctx, "probe", "target", []byte("old"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	return store, outer
}

func checkEmbeddedSaveBody(t *testing.T, wrapped, together bool) {
	t.Helper()
	ctx := context.Background()
	store, outer := embeddedSaveBodyFixture(t)
	store.bodyErr = storage.ErrObjectNotFound
	store.together = together
	if wrapped {
		store.bodyErr = fmt.Errorf("body: %w", store.bodyErr)
	}
	object, condition, err := outer.ReadForSave(ctx, "probe", "target")
	if !errors.Is(err, ErrObjectNotFound) || condition.ObservedAbsence() || len(object.Data) != 0 {
		t.Errorf("public body failure became observation: object=%+v absent=%t error=%v", object, condition.ObservedAbsence(), err)
	}
	if store.bytes != 1 || store.closes != 1 {
		t.Fatalf("public reader bytes=%d closes=%d", store.bytes, store.closes)
	}
	store.bodyErr = nil
	result, err := outer.SaveObject(ctx, "probe", "target", []byte("bad"), SaveOptions{Condition: condition})
	if !errors.Is(err, ErrInvalidSaveCondition) || result.Outcome != SaveNotCommitted {
		t.Errorf("public failure issued usable condition: result=%+v error=%v", result, err)
	}
	object, condition, err = outer.ReadForSave(ctx, "probe", "target")
	if err != nil || condition.ObservedAbsence() || string(object.Data) != "old" {
		t.Fatalf("healthy public observation: object=%+v absent=%t error=%v", object, condition.ObservedAbsence(), err)
	}
	if result, err := outer.SaveObject(ctx, "probe", "target", []byte("12345678"), SaveOptions{Condition: condition}); err != nil || result.Outcome != SaveCommitted {
		t.Fatalf("healthy public save: result=%+v error=%v", result, err)
	}
}
