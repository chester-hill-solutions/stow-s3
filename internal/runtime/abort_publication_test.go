package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type abortFaultStore struct {
	*storage.MemoryStore
	failure error
	refuse  bool
	calls   int
}

func (s *abortFaultStore) AbortMultipartUpload(ctx context.Context, id string) error {
	s.calls++
	if s.refuse {
		return s.failure
	}
	if err := s.MemoryStore.AbortMultipartUpload(ctx, id); err != nil {
		return err
	}
	return s.failure
}

func newAbortFaultInstance(t *testing.T, exists bool, maxUploads int64) (*abortFaultStore, *Instance) {
	t.Helper()
	store := &abortFaultStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: maxUploads}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	if exists {
		if _, err := instance.PutObject(context.Background(), "probe", "target", []byte("old"), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return store, instance
}

func nativeAbortUpload(t *testing.T, instance *Instance, body string) string {
	t.Helper()
	upload, err := instance.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.UploadPart(context.Background(), upload.UploadID, 1, bytes.NewBufferString(body)); err != nil {
		t.Fatal(err)
	}
	return upload.UploadID
}

func TestAbortPublicationAccounting(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, outcome := range []string{"success", "committed", "committed-missing", "refused"} {
			t.Run(fmt.Sprintf("exists=%t/%s", exists, outcome), func(t *testing.T) { checkAbortPublication(t, exists, outcome) })
		}
	}
}

func checkAbortPublication(t *testing.T, exists bool, outcome string) {
	t.Helper()
	ctx := context.Background()
	store, instance := newAbortFaultInstance(t, exists, 1)
	id := nativeAbortUpload(t, instance, "part")
	switch outcome {
	case "committed":
		store.failure = storage.CommittedError(errors.New("after abort"))
	case "committed-missing":
		store.failure = storage.CommittedError(storage.ErrUploadNotFound)
	case "refused":
		store.failure, store.refuse = storage.ErrPreconditionFailed, true
	}
	if err := instance.AbortMultipartUpload(ctx, id); !errors.Is(err, store.failure) || store.calls != 1 {
		t.Fatalf("abort error=%v calls=%d", err, store.calls)
	}
	if store.refuse {
		assertNativeAbortState(t, instance, exists, true)
		if _, err := instance.PutObject(ctx, "probe", "target", []byte("12345"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("refusal released retained part bytes: %v", err)
		}
		store.refuse, store.failure = false, nil
		if err := instance.AbortMultipartUpload(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	assertNativeAbortState(t, instance, exists, false)
	if _, err := store.GetMultipartUpload(ctx, id); !errors.Is(err, storage.ErrUploadNotFound) {
		t.Errorf("abort retained actual upload: %v", err)
	}
	calls := store.calls
	if err := instance.AbortMultipartUpload(ctx, id); !errors.Is(err, storage.ErrUploadNotFound) || store.calls != calls {
		t.Errorf("repeat abort error=%v provider calls=%d", err, store.calls-calls)
	}
	checkNativeAbortCapacity(t, instance, store)
}

func assertNativeAbortState(t *testing.T, instance *Instance, exists, held bool) {
	t.Helper()
	var wantObjects, wantBytes int64
	wantSessions := 0
	if held {
		wantSessions = 1
		wantBytes = 4
		if !exists {
			wantObjects = 1
		}
	}
	wantUsage := Usage{}
	if exists {
		wantUsage = Usage{Bytes: 3, Objects: 1}
	}
	if instance.reservedBytes != wantBytes || instance.reservedObjects != wantObjects || len(instance.multipart) != wantSessions || instance.Usage() != wantUsage {
		t.Errorf("abort state bytes=%d objects=%d sessions=%d usage=%+v want bytes=%d objects=%d sessions=%d usage=%+v", instance.reservedBytes, instance.reservedObjects, len(instance.multipart), instance.Usage(), wantBytes, wantObjects, wantSessions, wantUsage)
	}
	if exists {
		object, err := instance.GetObject(context.Background(), "probe", "target")
		if err != nil || string(object.Data) != "old" {
			t.Fatalf("abort changed existing object: %+v %v", object, err)
		}
	}
}

func checkNativeAbortCapacity(t *testing.T, instance *Instance, store *abortFaultStore) {
	t.Helper()
	ctx := context.Background()
	if err := instance.DeleteObject(ctx, "probe", "target"); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "probe", "other", []byte("12345678"), PutOptions{}); err != nil {
		t.Errorf("abort left byte/object capacity: %v", err)
	}
	store.failure = nil
	next, err := instance.CreateMultipartUpload(ctx, "probe", "other", storage.MultipartOptions{})
	if err != nil {
		t.Errorf("abort left session capacity: %v", err)
		return
	}
	if err := instance.AbortMultipartUpload(ctx, next.UploadID); err != nil {
		t.Fatal(err)
	}
}

func TestCommittedAbortKeepsSharedReservation(t *testing.T) {
	store, instance := newAbortFaultInstance(t, false, 2)
	first := nativeAbortUpload(t, instance, "part")
	second := nativeAbortUpload(t, instance, "xx")
	store.failure = storage.CommittedError(errors.New("after abort"))
	if err := instance.AbortMultipartUpload(context.Background(), first); !errors.Is(err, store.failure) {
		t.Fatal(err)
	}
	if instance.reservedBytes != 2 || instance.reservedObjects != 1 || len(instance.multipart) != 1 {
		t.Errorf("shared abort bytes=%d objects=%d sessions=%d", instance.reservedBytes, instance.reservedObjects, len(instance.multipart))
	}
	if _, err := instance.PutObject(context.Background(), "probe", "target", []byte("1234567"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("surviving part bytes escaped quota: %v", err)
	}
	third, err := instance.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("first abort did not release a session: %v", err)
	}
	for _, id := range []string{third.UploadID, second} {
		if err := instance.AbortMultipartUpload(context.Background(), id); !errors.Is(err, store.failure) {
			t.Fatal(err)
		}
	}
	if instance.reservedBytes != 0 || instance.reservedObjects != 0 || len(instance.multipart) != 0 {
		t.Fatalf("final shared abort leaked reservations: %+v", instance.usageState)
	}
	if _, err := instance.PutObject(context.Background(), "probe", "other", []byte("12345678"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkAbortAccountingLifecycle(b *testing.B) {
	for _, outcome := range []string{"success", "committed"} {
		b.Run(outcome, func(b *testing.B) {
			var failure error
			if outcome == "committed" {
				failure = storage.CommittedError(errors.New("after abort"))
			}
			upload := &storage.MultipartUpload{UploadID: "upload", Bucket: "probe", Key: "target"}
			part := &storage.PartInfo{PartNumber: 1, Size: 4}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				state := newUsageState(Options{})
				state.finishUpload(upload, false, nil)
				state.finishPart(upload.UploadID, part, nil)
				state.finishAbort(upload.UploadID, false, failure)
			}
		})
	}
}
