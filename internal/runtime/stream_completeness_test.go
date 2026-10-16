package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func openCompletionStream(instance *Instance, adapter bool) (io.ReadCloser, Object, error) {
	if !adapter {
		return instance.OpenObject(context.Background(), "probe", "target")
	}
	bridge, err := NewStoreAdapter(instance)
	if err != nil {
		return nil, Object{}, err
	}
	reader, meta, err := bridge.GetObject(context.Background(), "probe", "target")
	if err != nil {
		return reader, Object{}, err
	}
	return reader, objectFromMeta(meta, nil), nil
}

func TestStreamEOFCompleteness(t *testing.T) {
	for _, adapter := range []bool{false, true} {
		for _, together := range []bool{false, true} {
			for after := 0; after <= 3; after++ {
				t.Run(fmt.Sprintf("adapter=%t/together=%t/after=%d", adapter, together, after), func(t *testing.T) {
					want := io.ErrUnexpectedEOF
					if after == 3 {
						want = nil
					}
					checkStreamCompletion(t, saveBodyFault{after: after, together: together, err: io.EOF}, adapter, 3, want)
				})
			}
		}
	}
}

func TestStreamEOFControls(t *testing.T) {
	for _, adapter := range []bool{false, true} {
		t.Run(fmt.Sprintf("adapter=%t/unknown-size", adapter), func(t *testing.T) { checkStreamCompletion(t, saveBodyFault{after: 3, err: io.EOF}, adapter, -1, nil) })
		t.Run(fmt.Sprintf("adapter=%t/empty", adapter), func(t *testing.T) { checkStreamCompletion(t, saveBodyFault{err: io.EOF}, adapter, 0, nil) })
		for _, err := range []error{fmt.Errorf("body: %w", io.EOF), storage.ErrObjectNotFound, storage.CommittedError(storage.ErrObjectNotFound), errors.New("read failed")} {
			t.Run(fmt.Sprintf("adapter=%t/error=%v", adapter, err), func(t *testing.T) {
				checkStreamCompletion(t, saveBodyFault{after: 1, together: true, err: err}, adapter, 3, err)
			})
		}
	}
}

func checkStreamCompletion(t *testing.T, fault saveBodyFault, adapter bool, size int64, want error) {
	t.Helper()
	f, store := completionFixture(t, size)
	store.fault = &fault
	usage, revision := f.instance.Usage(), f.instance.usageRevision
	reader, object, err := openCompletionStream(f.instance, adapter)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if store.bytes != 0 || store.closes != 0 || store.gets != 1 || object.Size != size || object.VersionID != f.before.VersionID {
		t.Fatalf("open consumed body or changed identity: object=%+v store=%+v", object, store)
	}
	checkCompletionBody(t, reader, fault, want)
	assertSaveBodyCounts(t, store, fault.after, 1)
	if f.instance.Usage() != usage || f.instance.usageRevision != revision {
		t.Fatal("stream changed accounting")
	}
	store.fault = nil
	f.instance.store = store
	verifyStreamReadRecovery(t, f, size == 0)
}

func TestStreamPartialCloseDoesNotRequireEOF(t *testing.T) {
	f, store := saveBodyFixture(t)
	store.fault = &saveBodyFault{after: 2, err: io.EOF}
	reader, _, err := f.instance.OpenObject(context.Background(), "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 1)
	if n, err := reader.Read(data); n != 1 || err != nil {
		t.Fatalf("partial read: %d/%v", n, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	assertSaveBodyCounts(t, store, 1, 1)
}

func verifyStreamReadRecovery(t *testing.T, f *saveReadFixture, empty bool) {
	t.Helper()
	if !empty {
		verifySaveReadRecovery(t, f)
		return
	}
	object, condition, err := f.instance.ReadForSave(context.Background(), "probe", "target")
	if err != nil || len(object.Data) != 0 || object.VersionID != f.before.VersionID || condition.ObservedAbsence() {
		t.Fatalf("healthy empty retry: object=%+v error=%v", object, err)
	}
	result, err := f.instance.SaveObject(context.Background(), "probe", "target", []byte("12345678"), SaveOptions{Condition: condition})
	if err != nil || result.Outcome != SaveCommitted || f.instance.Usage() != (Usage{Bytes: 8, Objects: 1}) {
		t.Fatalf("healthy empty save: result=%+v error=%v", result, err)
	}
}

func completionFixture(t *testing.T, size int64) (*saveReadFixture, *saveBodyStore) {
	t.Helper()
	f, store := saveBodyFixture(t)
	if size == 0 {
		var err error
		f.before, err = f.instance.PutObject(context.Background(), "probe", "target", nil, PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	if size < 0 {
		f.instance.store = &readSizeHintStore{saveBodyStore: store, size: size}
	}
	return f, store
}

func checkCompletionBody(t *testing.T, reader io.ReadCloser, fault saveBodyFault, want error) {
	t.Helper()
	if n, err := reader.Read(nil); n != 0 || err != nil {
		t.Fatalf("zero read: %d/%v", n, err)
	}
	data, err := io.ReadAll(reader)
	if err != want || len(data) != fault.after {
		t.Errorf("stream bytes=%d error=%v want=%v", len(data), err, want)
	}
	if n, err := reader.Read(make([]byte, 1)); n != 0 || err != io.ErrClosedPipe {
		t.Errorf("terminal read: %d/%v", n, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}
