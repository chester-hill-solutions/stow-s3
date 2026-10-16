package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type reviewOpeningErrorStore struct {
	*storage.MemoryStore
	path          string
	failure       error
	closeErr      error
	reads         int
	nilReader     bool
	reader        *os.File
	opens, closes int
}

func (s *reviewOpeningErrorStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	meta, err := s.MemoryStore.HeadObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	if s.nilReader {
		return nil, nil, s.failure
	}
	file, err := os.Open(s.path)
	if err != nil {
		return nil, nil, err
	}
	s.reader = file
	s.opens++
	return &reviewOpeningErrorReader{File: file, owner: s}, meta, s.failure
}

type reviewOpeningErrorReader struct {
	*os.File
	owner *reviewOpeningErrorStore
}

func (r *reviewOpeningErrorReader) Read(p []byte) (int, error) {
	r.owner.reads++
	return r.File.Read(p)
}
func (r *reviewOpeningErrorReader) Close() error {
	r.owner.closes++
	err := r.File.Close()
	if r.owner.closeErr != nil {
		return r.owner.closeErr
	}
	return err
}

type reviewOpeningRangeErrorStore struct{ *reviewOpeningErrorStore }

func (s *reviewOpeningRangeErrorStore) OpenObjectRange(ctx context.Context, bucket, key string, _ storage.ObjectRange) (io.ReadCloser, *storage.ObjectMeta, error) {
	return s.GetObject(ctx, bucket, key)
}

func TestOpeningErrorClosesReader(t *testing.T) {
	for _, mode := range []string{"stream", "get", "guarded", "adapter", "fallback-range", "native-range"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := saveBodyFixture(t)
			file := filepath.Join(t.TempDir(), "body")
			if err := os.WriteFile(file, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("provider opening failed")
			store := &reviewOpeningErrorStore{MemoryStore: f.store.MemoryStore, path: file, failure: failure}
			f.instance.store = store
			if mode == "native-range" {
				f.instance.store = &reviewOpeningRangeErrorStore{store}
			}
			t.Cleanup(func() {
				if store.reader != nil {
					_ = store.reader.Close()
				}
			})
			usage := f.instance.Usage()
			var err error
			if mode == "native-range" || mode == "fallback-range" {
				reader, _, openErr := f.instance.OpenObjectRange(context.Background(), "probe", "target", storage.ObjectRange{Length: 1})
				err = openErr
				if reader != nil {
					_ = reader.Close()
				}
			} else {
				_, _, err = observeExcessRead(f.instance, mode)
			}
			if err != failure {
				t.Fatalf("original opening error changed: %v", err)
			}
			if store.opens != 1 || store.reads != 0 {
				t.Fatalf("opens=%d reads=%d", store.opens, store.reads)
			}
			if store.closes != 1 {
				_, statErr := store.reader.Stat()
				t.Errorf("opening error discarded reader: closes=%d file_still_open=%t", store.closes, statErr == nil)
			}
			if f.instance.Usage() != usage {
				t.Fatal("opening refusal changed usage")
			}
			_ = store.reader.Close()
			store.reader = nil
			f.instance.store = store.MemoryStore
			verifySaveReadRecovery(t, f)
		})
	}
}

type openingGuardCase struct {
	name    string
	failure error
	absent  bool
}

func TestOpeningErrorPreservesGuardClassification(t *testing.T) {
	for _, item := range []openingGuardCase{
		{"ordinary", errors.New("opening failed"), false},
		{"missing", storage.ErrObjectNotFound, true},
		{"marked-missing", storage.CommittedError(storage.ErrObjectNotFound), false},
		{"bucket-missing", storage.ErrBucketNotFound, false},
	} {
		for _, nilReader := range []bool{false, true} {
			name := item.name + "/reader"
			if nilReader {
				name = item.name + "/nil"
			}
			t.Run(name, func(t *testing.T) { checkOpeningGuardError(t, item, nilReader) })
		}
	}
}

func checkOpeningGuardError(t *testing.T, item openingGuardCase, nilReader bool) {
	t.Helper()
	f, _ := saveBodyFixture(t)
	path := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &reviewOpeningErrorStore{MemoryStore: f.store.MemoryStore, path: path, failure: item.failure, nilReader: nilReader, closeErr: storage.CommittedError(storage.ErrObjectNotFound)}
	f.instance.store = store
	t.Cleanup(func() {
		if store.reader != nil {
			_ = store.reader.Close()
		}
	})
	before := f.instance.Usage()
	object, condition, err := f.instance.ReadForSave(context.Background(), "probe", "target")
	assertOpeningGuardResult(t, item, object, condition, err)
	want := 1
	if nilReader {
		want = 0
	}
	if store.closes != want || store.reads != 0 {
		t.Errorf("closes=%d want=%d reads=%d", store.closes, want, store.reads)
	}
	if !nilReader {
		if _, err := store.reader.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("file remains open: %v", err)
		}
	}
	if f.instance.Usage() != before {
		t.Error("opening error changed usage")
	}
	f.instance.store = store.MemoryStore
	verifySaveReadRecovery(t, f)
}

func assertOpeningGuardResult(t *testing.T, item openingGuardCase, object Object, condition SaveCondition, err error) {
	t.Helper()
	if item.absent {
		if err != nil || !condition.ObservedAbsence() || object.Key != "target" {
			t.Errorf("absence contract: object=%+v condition=%+v error=%v", object, condition, err)
		}
	} else if err != item.failure || condition != (SaveCondition{}) || object.Bucket != "" {
		t.Errorf("error contract: object=%+v condition=%+v error=%v", object, condition, err)
	}
}
