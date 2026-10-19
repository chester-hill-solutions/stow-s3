package embedded

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type excessPublicStore struct {
	*embeddedSaveBodyStore
	spoof bool
}

func (s *excessPublicStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	reader, meta, err := s.embeddedSaveBodyStore.GetObject(ctx, bucket, key)
	if err == nil && s.spoof {
		meta.Size = 0
	}
	return reader, meta, err
}

func TestEmbeddedExcessRead(t *testing.T) {
	for _, mode := range []string{"stream", "get", "guarded"} {
		for _, together := range []bool{false, true} {
			t.Run(fmt.Sprintf("mode=%s/together=%t", mode, together), func(t *testing.T) { checkEmbeddedExcessRead(t, mode, together) })
		}
	}
}

func checkEmbeddedExcessRead(t *testing.T, mode string, together bool) {
	t.Helper()
	ctx := context.Background()
	store, outer, before := excessPublicFixture(t)
	store.spoof, store.bodyErr, store.together = true, io.EOF, together
	object, condition, err := observePublicExcess(t, outer, mode)
	if err == nil || len(object.Data) != 0 || condition != (SaveCondition{}) {
		t.Errorf("public excess became observation: object=%+v error=%v", object, err)
	}
	if mode != "stream" && !reflect.ValueOf(object).IsZero() {
		t.Errorf("public malformed object returned: %+v", object)
	}
	if store.bytes != 1 || store.closes != 1 {
		t.Fatalf("provider bytes=%d closes=%d", store.bytes, store.closes)
	}
	store.spoof, store.bodyErr = false, nil
	healthy, condition, err := outer.ReadForSave(ctx, "probe", "target")
	if err != nil || string(healthy.Data) != "old" || healthy.ETag != before.ETag {
		t.Fatalf("healthy retry: object=%+v error=%v", healthy, err)
	}
	if result, err := outer.SaveObject(ctx, "probe", "target", []byte("12345678"), SaveOptions{Condition: condition}); err != nil || result.Outcome != SaveCommitted {
		t.Fatalf("healthy full-capacity save: result=%+v error=%v", result, err)
	}
}

func excessPublicFixture(t *testing.T) (*excessPublicStore, *Runtime, Object) {
	t.Helper()
	ctx := context.Background()
	store := &excessPublicStore{embeddedSaveBodyStore: &embeddedSaveBodyStore{MemoryStore: storage.NewMemoryStore()}}
	inner, err := stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendMemory, MaxBytes: 8, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	outer := &Runtime{inner: inner}
	t.Cleanup(func() { _ = outer.Close() })
	if err := outer.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	before, err := outer.PutObject(ctx, "probe", "target", []byte("old"), PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return store, outer, before
}

func observePublicExcess(t *testing.T, outer *Runtime, mode string) (Object, SaveCondition, error) {
	t.Helper()
	ctx := context.Background()
	var err error
	var object Object
	var condition SaveCondition
	if mode == "stream" {
		var reader io.ReadCloser
		reader, object, err = outer.OpenObject(ctx, "probe", "target")
		if err != nil {
			t.Fatal(err)
		}
		object.Data, err = io.ReadAll(reader)
		if closeErr := reader.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	} else if mode == "get" {
		object, err = outer.GetObject(ctx, "probe", "target")
	} else {
		object, condition, err = outer.ReadForSave(ctx, "probe", "target")
	}
	return object, condition, err
}
