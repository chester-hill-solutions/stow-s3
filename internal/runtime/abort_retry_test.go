package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type wrappedMissingAbortStore struct {
	*abortFaultStore
	missing error
}

func (s *wrappedMissingAbortStore) AbortMultipartUpload(ctx context.Context, id string) error {
	err := s.abortFaultStore.AbortMultipartUpload(ctx, id)
	if errors.Is(err, storage.ErrUploadNotFound) {
		return s.missing
	}
	return err
}

func TestAbortRetryAccounting(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, outcome := range []string{"missing", "wrapped-missing", "unknown", "refused"} {
			t.Run(fmt.Sprintf("exists=%t/%s", exists, outcome), func(t *testing.T) { checkAbortRetry(t, exists, outcome) })
		}
	}
}

func checkAbortRetry(t *testing.T, exists bool, outcome string) {
	t.Helper()
	ctx := context.Background()
	store, instance := newAbortFaultInstance(t, exists, 1)
	id := nativeAbortUpload(t, instance, "part")
	held := outcome == "unknown" || outcome == "refused"
	store.failure, store.refuse = errors.New("abort acknowledgement unavailable"), held
	if err := instance.AbortMultipartUpload(ctx, id); err != store.failure {
		t.Fatal(err)
	}
	assertNativeAbortState(t, instance, exists, true)
	want := storage.ErrUploadNotFound
	switch outcome {
	case "wrapped-missing":
		want = fmt.Errorf("abort: %w", storage.ErrUploadNotFound)
		instance.multipartStore = &wrappedMissingAbortStore{abortFaultStore: store, missing: want}
	case "unknown":
		want = errors.New("abort outcome unavailable")
		store.failure = want
	case "refused":
		want = storage.ErrPreconditionFailed
		store.failure = want
	}
	if err := instance.AbortMultipartUpload(ctx, id); err != want {
		t.Fatalf("retry error=%v want identical %v", err, want)
	}
	assertNativeAbortState(t, instance, exists, held)
	if held {
		recoverRetainedAbort(t, instance, store, id)
	}
	assertNativeAbortState(t, instance, exists, false)
	if _, err := store.GetMultipartUpload(ctx, id); !errors.Is(err, storage.ErrUploadNotFound) {
		t.Fatalf("backing upload retained: %v", err)
	}
	calls := store.calls
	if err := instance.AbortMultipartUpload(ctx, id); !errors.Is(err, storage.ErrUploadNotFound) || store.calls != calls {
		t.Errorf("repeat error=%v extra provider calls=%d", err, store.calls-calls)
	}
	checkNativeAbortCapacity(t, instance, store)
}

func recoverRetainedAbort(t *testing.T, instance *Instance, store *abortFaultStore, id string) {
	t.Helper()
	ctx := context.Background()
	parts, err := store.ListParts(ctx, id)
	if err != nil || len(parts) != 1 || parts[0].Size != 4 {
		t.Fatalf("retry lost retained part: %+v %v", parts, err)
	}
	if _, err := instance.PutObject(ctx, "probe", "target", []byte("12345"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("retry released retained bytes: %v", err)
	}
	store.failure, store.refuse = nil, false
	if err := instance.AbortMultipartUpload(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestMissingAbortKeepsSharedReservation(t *testing.T) {
	ctx := context.Background()
	store, instance := newAbortFaultInstance(t, false, 2)
	first := nativeAbortUpload(t, instance, "part")
	second := nativeAbortUpload(t, instance, "xx")
	store.failure = errors.New("abort acknowledgement unavailable")
	if err := instance.AbortMultipartUpload(ctx, first); err != store.failure {
		t.Fatal(err)
	}
	if err := instance.AbortMultipartUpload(ctx, first); err != storage.ErrUploadNotFound {
		t.Fatal(err)
	}
	if instance.reservedBytes != 2 || instance.reservedObjects != 1 || len(instance.multipart) != 1 {
		t.Errorf("shared bytes=%d objects=%d sessions=%d", instance.reservedBytes, instance.reservedObjects, len(instance.multipart))
	}
	if _, err := instance.PutObject(ctx, "probe", "target", []byte("1234567"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("surviving bytes escaped quota: %v", err)
	}
	if _, err := instance.PutObject(ctx, "probe", "other", nil, PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("surviving object slot escaped quota: %v", err)
	}
	third, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("confirmed absence stranded session: %v", err)
	}
	store.failure = nil
	for _, id := range []string{third.UploadID, second} {
		if err := instance.AbortMultipartUpload(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if instance.reservedBytes != 0 || instance.reservedObjects != 0 || len(instance.multipart) != 0 {
		t.Fatalf("final abort retained quota: %+v", instance.usageState)
	}
}

func TestConcurrentMissingAbortRecovery(t *testing.T) {
	ctx := context.Background()
	store, instance := newAbortFaultInstance(t, false, 1)
	id := nativeAbortUpload(t, instance, "part")
	store.failure = errors.New("abort acknowledgement unavailable")
	if err := instance.AbortMultipartUpload(ctx, id); err != store.failure {
		t.Fatal(err)
	}
	calls := store.calls
	start := make(chan struct{})
	done := make(chan error, 2)
	for n := 0; n < 2; n++ {
		go func() { <-start; done <- instance.AbortMultipartUpload(ctx, id) }()
	}
	close(start)
	for n := 0; n < 2; n++ {
		if err := <-done; err != storage.ErrUploadNotFound {
			t.Errorf("retry error=%v", err)
		}
	}
	if store.calls != calls+1 {
		t.Errorf("provider retry calls=%d want one", store.calls-calls)
	}
	assertNativeAbortState(t, instance, false, false)
	checkNativeAbortCapacity(t, instance, store)
}

func BenchmarkAbortRetryAccountingLifecycle(b *testing.B) {
	for _, outcome := range []string{"missing", "unknown"} {
		b.Run(outcome, func(b *testing.B) {
			failure := errors.New("abort acknowledgement unavailable")
			confirmation := failure
			if outcome == "missing" {
				confirmation = storage.ErrUploadNotFound
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
				state.finishAbort(upload.UploadID, false, confirmation)
			}
		})
	}
}
