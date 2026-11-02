package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type saveReadFaultStore struct {
	*storage.MemoryStore
	readErr      error
	bodyFailure  bool
	gets, closes int
}

func (s *saveReadFaultStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	s.gets++
	if s.readErr != nil && !s.bodyFailure {
		return nil, nil, s.readErr
	}
	reader, meta, err := s.MemoryStore.GetObject(ctx, bucket, key)
	if err != nil || s.readErr == nil {
		return reader, meta, err
	}
	return &saveReadFaultBody{ReadCloser: reader, store: s}, meta, nil
}

type saveReadFaultBody struct {
	io.ReadCloser
	store *saveReadFaultStore
	read  bool
}

func (r *saveReadFaultBody) Read(p []byte) (int, error) {
	if r.read {
		return 0, r.store.readErr
	}
	r.read = true
	return r.ReadCloser.Read(p)
}
func (r *saveReadFaultBody) Close() error { r.store.closes++; return r.ReadCloser.Close() }

type saveReadFixture struct {
	store    *saveReadFaultStore
	instance *Instance
	before   Object
	exists   bool
}

func newSaveReadFixture(t *testing.T, exists bool) *saveReadFixture {
	t.Helper()
	ctx := context.Background()
	store := &saveReadFaultStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 8, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	f := &saveReadFixture{store: store, instance: instance, exists: exists}
	if exists {
		f.before, err = instance.PutObject(ctx, "probe", "target", []byte("old"), PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestReadForSaveMarkedMissingObservation(t *testing.T) {
	for _, order := range []string{"marker-first", "missing-first", "wrapped-marker-first", "wrapped-missing-first"} {
		for _, mode := range []string{"open-present", "open-absent", "body-present"} {
			t.Run(order+"/"+mode, func(t *testing.T) {
				f := newSaveReadFixture(t, mode != "open-absent")
				f.store.readErr, f.store.bodyFailure = objectHeadAmbiguity(order), mode == "body-present"
				assertSaveReadRefusal(t, f)
			})
		}
	}
}

func assertSaveReadRefusal(t *testing.T, f *saveReadFixture) {
	t.Helper()
	ctx := context.Background()
	usage, revision := f.instance.Usage(), f.instance.usageRevision
	object, condition, err := f.instance.ReadForSave(ctx, "probe", "target")
	if err != f.store.readErr {
		t.Errorf("read lost original error: %v want %v", err, f.store.readErr)
	}
	if condition != (SaveCondition{}) || object.Bucket != "" || len(object.Data) != 0 || object.Metadata != nil {
		t.Errorf("unknown read issued object/condition: object=%+v condition=%+v", object, condition)
	}
	wantCloses := 0
	if f.store.bodyFailure {
		wantCloses = 1
	}
	if f.store.gets != 1 || f.store.closes != wantCloses {
		t.Errorf("provider calls=%d closes=%d want 1/%d", f.store.gets, f.store.closes, wantCloses)
	}
	if f.instance.Usage() != usage || f.instance.usageRevision != revision {
		t.Errorf("read changed ledger: usage=%+v revision=%d", f.instance.Usage(), f.instance.usageRevision)
	}
	f.store.readErr = nil
	result, saveErr := f.instance.SaveObject(ctx, "probe", "target", []byte("bad"), SaveOptions{Condition: condition})
	if !errors.Is(saveErr, ErrInvalidSaveCondition) || result.Outcome != SaveNotCommitted {
		t.Errorf("uncertain read produced usable condition: result=%+v error=%v", result, saveErr)
	}
	verifySaveReadRecovery(t, f)
}

func verifySaveReadRecovery(t *testing.T, f *saveReadFixture) {
	t.Helper()
	ctx := context.Background()
	object, condition, err := f.instance.ReadForSave(ctx, "probe", "target")
	if err != nil || condition.ObservedAbsence() == f.exists {
		t.Fatalf("healthy observation: object=%+v condition=%+v error=%v", object, condition, err)
	}
	if f.exists && (string(object.Data) != "old" || object.VersionID != f.before.VersionID || object.ETag != f.before.ETag || object.ContentType != "text/plain" || object.Metadata["state"] != "original") {
		t.Fatalf("unknown read changed object: %+v", object)
	}
	result, err := f.instance.SaveObject(ctx, "probe", "target", []byte("12345678"), SaveOptions{Condition: condition})
	if err != nil || result.Outcome != SaveCommitted || f.instance.Usage() != (Usage{Bytes: 8, Objects: 1}) {
		t.Fatalf("healthy save/capacity: result=%+v error=%v usage=%+v", result, err, f.instance.Usage())
	}
	result, err = f.instance.SaveObject(ctx, "probe", "target", []byte("stale"), SaveOptions{Condition: condition})
	if !errors.Is(err, storage.ErrSaveConflict) || result.Outcome != SaveNotCommitted {
		t.Errorf("stale condition accepted: result=%+v error=%v", result, err)
	}
}

func TestReadForSaveObservationControls(t *testing.T) {
	unknown := errors.New("read unavailable")
	for _, item := range []struct {
		name string
		err  error
		body bool
	}{
		{"unknown-open", unknown, false},
		{"unknown-body", unknown, true},
		{"marker-only-open", errors.Join(storage.ErrMutationCommitted, unknown), false},
		{"marker-only-body", errors.Join(storage.ErrMutationCommitted, unknown), true},
	} {
		t.Run(item.name, func(t *testing.T) {
			f := newSaveReadFixture(t, true)
			f.store.readErr, f.store.bodyFailure = item.err, item.body
			assertSaveReadRefusal(t, f)
		})
	}
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing/wrapped=%t", wrapped), func(t *testing.T) {
			f := newSaveReadFixture(t, false)
			f.store.readErr = storage.ErrObjectNotFound
			if wrapped {
				f.store.readErr = fmt.Errorf("open: %w", f.store.readErr)
			}
			verifySaveReadRecovery(t, f)
		})
	}
	for _, bucket := range []string{"missing", "probe"} {
		t.Run("healthy-"+bucket, func(t *testing.T) {
			f := newSaveReadFixture(t, true)
			if bucket == "probe" {
				verifySaveReadRecovery(t, f)
				return
			}
			object, condition, err := f.instance.ReadForSave(context.Background(), bucket, "target")
			if err != storage.ErrBucketNotFound || condition != (SaveCondition{}) || object.Bucket != "" {
				t.Fatalf("missing bucket became absence: object=%+v condition=%+v error=%v", object, condition, err)
			}
		})
	}
}
