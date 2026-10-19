package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type saveBodyFault struct {
	after    int
	together bool
	err      error
}
type saveBodyStore struct {
	*storage.MemoryStore
	fault               *saveBodyFault
	gets, bytes, closes int
}

func (s *saveBodyStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	s.gets++
	reader, meta, err := s.MemoryStore.GetObject(ctx, bucket, key)
	if err != nil || s.fault == nil {
		return reader, meta, err
	}
	return &saveBodyReader{ReadCloser: reader, store: s, remaining: s.fault.after}, meta, nil
}

type saveBodyReader struct {
	io.ReadCloser
	store     *saveBodyStore
	remaining int
}

func (r *saveBodyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		return 0, r.store.fault.err
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.ReadCloser.Read(p)
	r.remaining -= n
	r.store.bytes += n
	if r.remaining == 0 && r.store.fault.together {
		return n, r.store.fault.err
	}
	return n, err
}
func (r *saveBodyReader) Close() error { r.store.closes++; return r.ReadCloser.Close() }

func saveBodyFixture(t *testing.T) (*saveReadFixture, *saveBodyStore) {
	t.Helper()
	f := newSaveReadFixture(t, true)
	store := &saveBodyStore{MemoryStore: f.store.MemoryStore}
	f.instance.store = store
	return f, store
}

func TestReadForSaveBodyMissingDoesNotEstablishAbsence(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		for _, after := range []int{0, 1, 3} {
			for _, together := range []bool{false, true} {
				t.Run(fmt.Sprintf("wrapped=%t/after=%d/together=%t", wrapped, after, together), func(t *testing.T) {
					err := storage.ErrObjectNotFound
					if wrapped {
						err = fmt.Errorf("body: %w", err)
					}
					checkSaveBodyFailure(t, saveBodyFault{after: after, together: together, err: err})
				})
			}
		}
	}
}

func checkSaveBodyFailure(t *testing.T, fault saveBodyFault) {
	t.Helper()
	ctx := context.Background()
	f, store := saveBodyFixture(t)
	store.fault = &fault
	usage, revision := f.instance.Usage(), f.instance.usageRevision
	object, condition, err := f.instance.ReadForSave(ctx, "probe", "target")
	if err != fault.err || condition != (SaveCondition{}) || object.Bucket != "" || len(object.Data) != 0 {
		t.Errorf("body error became observation: object=%+v condition=%+v error=%v want=%v", object, condition, err, fault.err)
	}
	assertSaveBodyCounts(t, store, fault.after, 1)
	if f.instance.Usage() != usage || f.instance.usageRevision != revision {
		t.Errorf("read changed usage/revision: %+v/%d", f.instance.Usage(), f.instance.usageRevision)
	}
	object, err = f.instance.GetObject(ctx, "probe", "target")
	if err != fault.err || len(object.Data) != 0 || object.Bucket != "" {
		t.Errorf("ordinary GET contract changed: object=%+v error=%v", object, err)
	}
	assertSaveBodyCounts(t, store, fault.after, 2)
	store.fault = nil
	result, err := f.instance.SaveObject(ctx, "probe", "target", []byte("bad"), SaveOptions{Condition: condition})
	if !errors.Is(err, ErrInvalidSaveCondition) || result.Outcome != SaveNotCommitted {
		t.Errorf("failed body issued usable condition: result=%+v error=%v", result, err)
	}
	verifySaveReadRecovery(t, f)
}

func TestReadForSaveBodyFailureControls(t *testing.T) {
	for _, item := range []struct {
		name string
		err  error
	}{
		{"unexpected-eof", io.ErrUnexpectedEOF},
		{"unknown", errors.New("body unavailable")},
		{"bucket-missing", storage.ErrBucketNotFound},
		{"marked-missing", storage.CommittedError(storage.ErrObjectNotFound)},
	} {
		t.Run(item.name, func(t *testing.T) { checkSaveBodyFailure(t, saveBodyFault{after: 1, together: true, err: item.err}) })
	}
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("complete/empty=%t", empty), func(t *testing.T) { checkCompleteSaveBody(t, empty) })
	}
}

func checkCompleteSaveBody(t *testing.T, empty bool) {
	t.Helper()
	ctx := context.Background()
	f, store := saveBodyFixture(t)
	data := "old"
	if empty {
		data = ""
		var err error
		f.before, err = f.instance.PutObject(ctx, "probe", "target", nil, PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	object, err := f.instance.GetObject(ctx, "probe", "target")
	if err != nil || string(object.Data) != data || object.VersionID != f.before.VersionID {
		t.Fatalf("complete GET: object=%+v error=%v", object, err)
	}
	object, condition, err := f.instance.ReadForSave(ctx, "probe", "target")
	if err != nil || condition.ObservedAbsence() || string(object.Data) != data {
		t.Fatalf("complete guarded read: object=%+v absent=%t error=%v", object, condition.ObservedAbsence(), err)
	}
	if store.gets != 2 {
		t.Fatalf("complete GET calls=%d", store.gets)
	}
	result, err := f.instance.SaveObject(ctx, "probe", "target", []byte("12345678"), SaveOptions{Condition: condition})
	if err != nil || result.Outcome != SaveCommitted || f.instance.Usage() != (Usage{Bytes: 8, Objects: 1}) {
		t.Fatalf("complete guard/capacity: result=%+v error=%v usage=%+v", result, err, f.instance.Usage())
	}
}

func assertSaveBodyCounts(t testing.TB, store *saveBodyStore, after, operations int) {
	t.Helper()
	if store.gets != operations || store.closes != operations || store.bytes != after*operations {
		t.Fatalf("reader gets=%d closes=%d bytes=%d want %d/%d/%d", store.gets, store.closes, store.bytes, operations, operations, after*operations)
	}
}
