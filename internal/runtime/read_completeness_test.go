package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func readCompletionObservation(instance *Instance, guarded bool) (Object, SaveCondition, error) {
	if guarded {
		return instance.ReadForSave(context.Background(), "probe", "target")
	}
	object, err := instance.GetObject(context.Background(), "probe", "target")
	return object, SaveCondition{}, err
}

func TestBufferedReadRejectsPrematureEOF(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		for _, after := range []int{0, 1, 2} {
			for _, together := range []bool{false, true} {
				t.Run(fmt.Sprintf("guarded=%t/after=%d/together=%t", guarded, after, together), func(t *testing.T) {
					checkPrematureEOF(t, guarded, after, together)
				})
			}
		}
	}
}

func checkPrematureEOF(t *testing.T, guarded bool, after int, together bool) {
	t.Helper()
	f, store := saveBodyFixture(t)
	store.fault = &saveBodyFault{after: after, together: together, err: io.EOF}
	usage, revision := f.instance.Usage(), f.instance.usageRevision
	object, condition, err := readCompletionObservation(f.instance, guarded)
	if err != io.ErrUnexpectedEOF || !reflect.ValueOf(object).IsZero() || condition != (SaveCondition{}) {
		t.Errorf("premature EOF became observation: object=%+v condition=%+v error=%v", object, condition, err)
	}
	assertSaveBodyCounts(t, store, after, 1)
	if f.instance.Usage() != usage || f.instance.usageRevision != revision {
		t.Errorf("incomplete read changed usage/revision: %+v/%d", f.instance.Usage(), f.instance.usageRevision)
	}
	store.fault = nil
	result, err := f.instance.SaveObject(context.Background(), "probe", "target", []byte("bad"), SaveOptions{Condition: condition})
	if !errors.Is(err, ErrInvalidSaveCondition) || result.Outcome != SaveNotCommitted {
		t.Errorf("incomplete body issued usable condition: result=%+v error=%v", result, err)
	}
	verifySaveReadRecovery(t, f)
}

func TestBufferedReadCompleteEOF(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		for _, empty := range []bool{false, true} {
			for _, together := range []bool{false, true} {
				t.Run(fmt.Sprintf("guarded=%t/empty=%t/together=%t", guarded, empty, together), func(t *testing.T) {
					checkCompleteEOF(t, guarded, empty, together, false)
				})
			}
		}
	}
}

func TestBufferedReadUnknownSize(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		t.Run(fmt.Sprintf("guarded=%t", guarded), func(t *testing.T) { checkCompleteEOF(t, guarded, false, true, true) })
	}
}

func TestBufferedReadPreservesWrappedEOF(t *testing.T) {
	checkSaveBodyFailure(t, saveBodyFault{after: 1, together: true, err: fmt.Errorf("body: %w", io.EOF)})
}

type readSizeHintStore struct {
	*saveBodyStore
	size int64
}

func (s *readSizeHintStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	reader, meta, err := s.saveBodyStore.GetObject(ctx, bucket, key)
	if err == nil {
		meta.Size = s.size
	}
	return reader, meta, err
}

func checkCompleteEOF(t *testing.T, guarded, empty, together, unknown bool) {
	t.Helper()
	f, store := saveBodyFixture(t)
	body := "old"
	if empty {
		body = ""
		var err error
		f.before, err = f.instance.PutObject(context.Background(), "probe", "target", nil, PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	size := int64(len(body))
	if unknown {
		size = -1
		f.instance.store = &readSizeHintStore{saveBodyStore: store, size: size}
	}
	store.fault = &saveBodyFault{after: len(body), together: together, err: io.EOF}
	object, condition, err := readCompletionObservation(f.instance, guarded)
	if err != nil || string(object.Data) != body || object.Size != size || object.VersionID != f.before.VersionID || condition.ObservedAbsence() {
		t.Fatalf("complete observation changed: object=%+v condition=%+v error=%v", object, condition, err)
	}
	assertSaveBodyCounts(t, store, len(body), 1)
	store.fault = nil
	if !guarded {
		_, condition, err = f.instance.ReadForSave(context.Background(), "probe", "target")
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := f.instance.SaveObject(context.Background(), "probe", "target", []byte("12345678"), SaveOptions{Condition: condition})
	if err != nil || result.Outcome != SaveCommitted || f.instance.Usage() != (Usage{Bytes: 8, Objects: 1}) {
		t.Fatalf("complete guard/capacity: result=%+v error=%v usage=%+v", result, err, f.instance.Usage())
	}
}
