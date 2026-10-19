package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type initiationFaultStore struct {
	*storage.MemoryStore
	failure   error
	refuse    bool
	calls     int
	published *storage.MultipartUpload
}

func (s *initiationFaultStore) CreateMultipartUpload(ctx context.Context, bucket, key string, opts storage.MultipartOptions) (*storage.MultipartUpload, error) {
	s.calls++
	if s.refuse {
		return &storage.MultipartUpload{UploadID: "not-created"}, s.failure
	}
	upload, err := s.MemoryStore.CreateMultipartUpload(ctx, bucket, key, opts)
	if err != nil {
		return upload, err
	}
	s.published = upload
	return upload, s.failure
}

func TestInitiationPublicationAccounting(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, outcome := range []string{"success", "committed", "committed-missing", "refused"} {
			t.Run(fmt.Sprintf("exists=%t/%s", exists, outcome), func(t *testing.T) { checkInitiationPublication(t, exists, outcome) })
		}
	}
}

func newInitiationFaultInstance(t *testing.T, exists bool, maxUploads int64) (*initiationFaultStore, *Instance) {
	t.Helper()
	store := &initiationFaultStore{MemoryStore: storage.NewMemoryStore()}
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

func checkInitiationPublication(t *testing.T, exists bool, outcome string) {
	t.Helper()
	ctx := context.Background()
	store, instance := newInitiationFaultInstance(t, exists, 1)
	cause := errors.New("after upload initiation")
	switch outcome {
	case "committed":
		store.failure = storage.CommittedError(cause)
	case "committed-missing":
		store.failure = storage.CommittedError(errors.Join(cause, storage.ErrObjectNotFound))
	case "refused":
		store.failure, store.refuse = storage.ErrPreconditionFailed, true
	}
	opts := storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "init"}, ChecksumAlgorithm: "SHA256"}
	upload, err := instance.CreateMultipartUpload(ctx, "probe", "target", opts)
	if !errors.Is(err, store.failure) || store.calls != 1 {
		t.Fatalf("upload=%+v error=%v calls=%d", upload, err, store.calls)
	}
	if store.refuse {
		assertInitiationRefused(t, instance, upload)
		store.refuse, store.failure = false, nil
		upload, err = instance.CreateMultipartUpload(ctx, "probe", "target", opts)
		if err != nil {
			t.Fatal(err)
		}
	}
	assertNativeInitiationIdentity(t, instance, store.published, upload)
	opts.Metadata["state"] = "input"
	assertInitiationReservations(t, instance, exists)
	checkNativeInitiationRecovery(t, instance, store, upload)
}

func checkNativeInitiationRecovery(t *testing.T, instance *Instance, store *initiationFaultStore, upload *storage.MultipartUpload) {
	t.Helper()
	ctx := context.Background()
	calls := store.calls
	if _, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{}); !errors.Is(err, ErrQuotaExceeded) || store.calls != calls {
		t.Errorf("session quota error=%v calls=%d", err, store.calls-calls)
	}
	if _, err := instance.PutObject(ctx, "probe", "other", []byte("x"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("object reservation escaped quota: %v", err)
	}
	if upload == nil {
		return
	}
	if err := instance.AbortMultipartUpload(ctx, upload.UploadID); err != nil {
		t.Fatal(err)
	}
	if instance.reservedObjects != 0 || len(instance.multipart) != 0 {
		t.Fatalf("abort left object reservations=%d uploads=%d", instance.reservedObjects, len(instance.multipart))
	}
	store.failure = nil
	if _, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{}); err != nil {
		t.Fatalf("abort left session quota: %v", err)
	}
}

func assertInitiationRefused(t *testing.T, instance *Instance, upload *storage.MultipartUpload) {
	t.Helper()
	if upload != nil || len(instance.multipart) != 0 || instance.reservedObjects != 0 {
		t.Fatalf("ordinary refusal registered upload=%+v sessions=%d reservations=%d", upload, len(instance.multipart), instance.reservedObjects)
	}
	actual, err := instance.multipartStore.ListMultipartUploads(context.Background(), "probe", storage.MultipartListOptions{})
	if err != nil || len(actual.Uploads) != 0 {
		t.Fatalf("refusal stored uploads=%+v error=%v", actual, err)
	}
}

func assertNativeInitiationIdentity(t *testing.T, instance *Instance, want, got *storage.MultipartUpload) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("committed upload=%+v want %+v", got, want)
	}
	if got == nil {
		return
	}
	got.Options.Metadata["state"] = "caller"
	if want.Options.Metadata["state"] != "init" || instance.multipart[got.UploadID].upload.Options.Metadata["state"] != "init" {
		t.Error("returned upload aliases provider or accounting metadata")
	}
	stored, err := instance.multipartStore.GetMultipartUpload(context.Background(), got.UploadID)
	if err != nil || stored.Options.Metadata["state"] != "init" {
		t.Fatalf("initiation metadata changed: %+v %v", stored, err)
	}
	got.Options.Metadata["state"] = "init"
}

func assertInitiationReservations(t *testing.T, instance *Instance, exists bool) {
	t.Helper()
	wantObjects := int64(1)
	wantUsage := Usage{}
	if exists {
		wantObjects = 0
		wantUsage = Usage{Bytes: 3, Objects: 1}
	}
	if instance.reservedObjects != wantObjects || len(instance.multipart) != 1 || instance.reservedBytes != 0 || instance.Usage() != wantUsage {
		t.Errorf("initiation reservations objects=%d bytes=%d sessions=%d usage=%+v", instance.reservedObjects, instance.reservedBytes, len(instance.multipart), instance.Usage())
	}
	actual, err := instance.multipartStore.ListMultipartUploads(context.Background(), "probe", storage.MultipartListOptions{})
	if err != nil || len(actual.Uploads) != 1 {
		t.Errorf("actual uploads=%+v error=%v", actual, err)
	}
	for _, upload := range instance.multipart {
		if upload.upload.Options.Metadata["state"] != "init" {
			t.Error("input metadata aliases accounting")
		}
	}
}

func TestCommittedInitiationsShareTargetReservation(t *testing.T) {
	store, instance := newInitiationFaultInstance(t, false, 2)
	store.failure = storage.CommittedError(errors.New("after upload initiation"))
	var ids []string
	for range 2 {
		upload, err := instance.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{})
		if !errors.Is(err, store.failure) || upload == nil {
			t.Fatalf("committed initiation lost descriptor: %+v %v", upload, err)
		}
		ids = append(ids, upload.UploadID)
	}
	if ids[0] == ids[1] || instance.reservedObjects != 1 {
		t.Fatalf("shared target reservations=%d ids=%v", instance.reservedObjects, ids)
	}
	if _, err := instance.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{}); !errors.Is(err, ErrQuotaExceeded) || store.calls != 2 {
		t.Fatalf("session limit error=%v calls=%d", err, store.calls)
	}
	for n, id := range ids {
		if err := instance.AbortMultipartUpload(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if instance.reservedObjects != int64(1-n) {
			t.Fatalf("abort %d left object reservations=%d", n, instance.reservedObjects)
		}
	}
	if _, err := instance.PutObject(context.Background(), "probe", "other", []byte("12345678"), PutOptions{}); err != nil {
		t.Fatalf("shared reservation leaked: %v", err)
	}
}
