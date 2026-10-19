package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func observeExcessRead(instance *Instance, mode string) (Object, SaveCondition, error) {
	if mode == "get" || mode == "guarded" {
		return readCompletionObservation(instance, mode == "guarded")
	}
	reader, object, err := openCompletionStream(instance, mode == "adapter")
	if err != nil {
		return Object{}, SaveCondition{}, err
	}
	defer reader.Close()
	if mode == "copy" {
		var buffer bytes.Buffer
		_, err = io.Copy(&buffer, reader)
		object.Data = buffer.Bytes()
	} else {
		object.Data, err = io.ReadAll(reader)
	}
	return object, SaveCondition{}, err
}

func TestExcessReadRejected(t *testing.T) {
	for _, mode := range []string{"stream", "copy", "adapter", "get", "guarded"} {
		for _, size := range []int64{0, 2} {
			for _, together := range []bool{false, true} {
				t.Run(fmt.Sprintf("mode=%s/size=%d/together=%t", mode, size, together), func(t *testing.T) { checkExcessRead(t, mode, size, together) })
			}
		}
	}
}

func checkExcessRead(t *testing.T, mode string, size int64, together bool) {
	t.Helper()
	f, store := saveBodyFixture(t)
	f.instance.store = &readSizeHintStore{saveBodyStore: store, size: size}
	store.fault = &saveBodyFault{after: 3, together: together, err: io.EOF}
	usage, revision := f.instance.Usage(), f.instance.usageRevision
	object, condition, err := observeExcessRead(f.instance, mode)
	if err == nil || errors.Is(err, io.EOF) {
		t.Errorf("excess body accepted: data=%q size=%d error=%v", object.Data, object.Size, err)
	}
	checkExcessResult(t, mode, object, condition, "old"[:size])
	assertSaveBodyCounts(t, store, int(size)+1, 1)
	if f.instance.Usage() != usage || f.instance.usageRevision != revision {
		t.Fatal("malformed read changed accounting")
	}
	store.fault = nil
	f.instance.store = store
	if mode == "guarded" {
		result, err := f.instance.SaveObject(context.Background(), "probe", "target", []byte("bad"), SaveOptions{Condition: condition})
		if !errors.Is(err, ErrInvalidSaveCondition) || result.Outcome != SaveNotCommitted {
			t.Errorf("excess body issued usable condition: result=%+v error=%v", result, err)
		}
	}
	verifySaveReadRecovery(t, f)
}

func checkExcessResult(t *testing.T, mode string, object Object, condition SaveCondition, prefix string) {
	t.Helper()
	if mode == "get" || mode == "guarded" {
		if !reflect.ValueOf(object).IsZero() || condition != (SaveCondition{}) {
			t.Errorf("malformed body became observation: bytes=%d size=%d condition=%+v", len(object.Data), object.Size, condition)
		}
	} else if string(object.Data) != prefix || object.Size != int64(len(prefix)) {
		t.Errorf("stream exposed excess: bytes=%d size=%d want-bytes=%d", len(object.Data), object.Size, len(prefix))
	}
}

func TestExcessReadPreservesProviderError(t *testing.T) {
	for _, err := range []error{fmt.Errorf("body: %w", io.EOF), storage.ErrObjectNotFound, storage.CommittedError(storage.ErrObjectNotFound), errors.New("provider unavailable")} {
		t.Run(err.Error(), func(t *testing.T) {
			f, store := saveBodyFixture(t)
			f.instance.store = &readSizeHintStore{saveBodyStore: store, size: 2}
			store.fault = &saveBodyFault{after: 3, together: true, err: err}
			object, _, readErr := observeExcessRead(f.instance, "stream")
			if readErr != err || string(object.Data) != "ol" {
				t.Errorf("provider error changed or excess exposed: data=%q error=%v want=%v", object.Data, readErr, err)
			}
			assertSaveBodyCounts(t, store, 3, 1)
		})
	}
}

func TestExcessReadStopsLargeBody(t *testing.T) {
	for _, mode := range []string{"stream", "get", "guarded"} {
		t.Run(mode, func(t *testing.T) {
			f, store := saveBodyFixture(t)
			f.instance.store = &completionSeekStore{saveBodyStore: store, body: strings.Repeat("x", 1<<20)}
			object, condition, err := observeExcessRead(f.instance, mode)
			if err == nil {
				t.Error("large excess body accepted")
			}
			checkExcessResult(t, mode, object, condition, "xxx")
			assertSaveBodyCounts(t, store, 4, 1)
			f.instance.store = store
			verifySaveReadRecovery(t, f)
		})
	}
}
