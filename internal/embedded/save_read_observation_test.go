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

type embeddedSaveReadFaultStore struct {
	*storage.MemoryStore
	readErr error
}

func (s *embeddedSaveReadFaultStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	if s.readErr != nil {
		return nil, nil, s.readErr
	}
	return s.MemoryStore.GetObject(ctx, bucket, key)
}

func TestEmbeddedReadForSavePreservesMarkedMissing(t *testing.T) {
	for _, missingFirst := range []bool{false, true} {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("missingFirst=%t/wrapped=%t", missingFirst, wrapped), func(t *testing.T) { checkEmbeddedSaveRead(t, missingFirst, wrapped) })
		}
	}
}

func checkEmbeddedSaveRead(t *testing.T, missingFirst, wrapped bool) {
	t.Helper()
	ctx := context.Background()
	store, outer := embeddedSaveReadFixture(t)
	store.readErr = errors.Join(storage.ErrMutationCommitted, storage.ErrObjectNotFound)
	if missingFirst {
		store.readErr = errors.Join(storage.ErrObjectNotFound, storage.ErrMutationCommitted)
	}
	if wrapped {
		store.readErr = fmt.Errorf("get: %w", store.readErr)
	}
	object, condition, err := outer.ReadForSave(ctx, "probe", "target")
	if err != store.readErr || condition.ObservedAbsence() || len(object.Data) != 0 {
		t.Errorf("public read lost uncertainty: object=%+v absent=%t error=%v", object, condition.ObservedAbsence(), err)
	}
	store.readErr = nil
	result, err := outer.SaveObject(ctx, "probe", "target", []byte("bad"), SaveOptions{Condition: condition})
	if !errors.Is(err, ErrInvalidSaveCondition) || result.Outcome != SaveNotCommitted {
		t.Errorf("public invalid condition: result=%+v error=%v", result, err)
	}
	object, condition, err = outer.ReadForSave(ctx, "probe", "target")
	if err != nil || condition.ObservedAbsence() || string(object.Data) != "old" {
		t.Fatalf("healthy read: object=%+v absent=%t error=%v", object, condition.ObservedAbsence(), err)
	}
	result, err = outer.SaveObject(ctx, "probe", "target", []byte("12345678"), SaveOptions{Condition: condition})
	if err != nil || result.Outcome != SaveCommitted {
		t.Fatalf("healthy save: result=%+v error=%v", result, err)
	}
}

func embeddedSaveReadFixture(t *testing.T) (*embeddedSaveReadFaultStore, *Runtime) {
	t.Helper()
	ctx := context.Background()
	store := &embeddedSaveReadFaultStore{MemoryStore: storage.NewMemoryStore()}
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
