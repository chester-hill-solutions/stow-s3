package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type partFaultStore struct {
	*storage.MemoryStore
	failure   error
	refuse    bool
	calls     int
	published *storage.PartInfo
}

func (s *partFaultStore) UploadPart(ctx context.Context, upload string, number int, body io.Reader) (*storage.PartInfo, error) {
	s.calls++
	if s.refuse {
		return nil, s.failure
	}
	part, err := s.MemoryStore.UploadPart(ctx, upload, number, body)
	if err != nil {
		return part, err
	}
	s.published = part
	return part, s.failure
}

func TestPartPublicationAccounting(t *testing.T) {
	for _, oldSize := range []int{0, 2, 6} {
		for _, outcome := range []string{"success", "committed", "committed-missing", "refused"} {
			t.Run(fmt.Sprintf("old=%d/%s", oldSize, outcome), func(t *testing.T) { checkPartPublication(t, oldSize, outcome) })
		}
	}
}

func checkPartPublication(t *testing.T, oldSize int, outcome string) {
	t.Helper()
	ctx := context.Background()
	store, instance, upload := newPartFaultInstance(t, oldSize)
	cause := errors.New("after part publication")
	switch outcome {
	case "committed":
		store.failure = storage.CommittedError(cause)
	case "committed-missing":
		store.failure = storage.CommittedError(errors.Join(cause, storage.ErrObjectNotFound))
	case "refused":
		store.failure, store.refuse = storage.ErrInvalidPart, true
	}
	calls := store.calls
	part, err := instance.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("part")))
	if !errors.Is(err, store.failure) || store.calls != calls+1 {
		t.Fatalf("upload result=%+v error=%v calls=%d", part, err, store.calls-calls)
	}
	wantSize := int64(4)
	if store.refuse {
		wantSize = int64(oldSize)
		if part != nil {
			t.Errorf("refusal returned part: %+v", part)
		}
	} else {
		assertNativePublishedPart(t, store, part)
	}
	assertPartReservation(t, instance, upload.UploadID, wantSize)
	if instance.Usage() != (Usage{}) {
		t.Errorf("part upload changed object usage: %+v", instance.Usage())
	}
	checkPartQuotaRecovery(t, instance, store, upload.UploadID, wantSize)
}

func newPartFaultInstance(t *testing.T, oldSize int) (*partFaultStore, *Instance, *storage.MultipartUpload) {
	t.Helper()
	ctx := context.Background()
	store := &partFaultStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if oldSize > 0 {
		if _, err := instance.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(bytes.Repeat([]byte("o"), oldSize))); err != nil {
			t.Fatal(err)
		}
	}
	return store, instance, upload
}

func assertNativePublishedPart(t *testing.T, store *partFaultStore, part *storage.PartInfo) {
	t.Helper()
	if !reflect.DeepEqual(part, store.published) {
		t.Errorf("committed identity=%+v want %+v", part, store.published)
	}
	if part != nil {
		part.Size = 1000
		if store.published.Size != 4 {
			t.Error("returned part aliases the provider result")
		}
	}
}

func checkPartQuotaRecovery(t *testing.T, instance *Instance, store *partFaultStore, upload string, wantSize int64) {
	t.Helper()
	ctx := context.Background()
	calls := store.calls
	if _, err := instance.UploadPart(ctx, upload, 2, bytes.NewReader(bytes.Repeat([]byte("x"), int(9-wantSize)))); !errors.Is(err, ErrQuotaExceeded) || store.calls != calls {
		t.Errorf("part quota admission error=%v provider calls=%d", err, store.calls-calls)
	}
	if _, err := instance.PutObject(ctx, "probe", "target", bytes.Repeat([]byte("x"), int(9-wantSize)), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("stored parts escaped object admission: %v", err)
	}
	if _, err := instance.PutObject(ctx, "probe", "target", bytes.Repeat([]byte("x"), int(8-wantSize)), PutOptions{}); err != nil {
		t.Errorf("exact capacity refused: %v", err)
	}
	if err := instance.AbortMultipartUpload(ctx, upload); err != nil {
		t.Fatal(err)
	}
	if instance.reservedBytes != 0 {
		t.Fatalf("abort left reserved bytes=%d", instance.reservedBytes)
	}
	if _, err := instance.PutObject(ctx, "probe", "target", []byte("12345678"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{}); err != nil {
		t.Fatalf("abort did not release upload slot: %v", err)
	}
}

func assertPartReservation(t *testing.T, instance *Instance, upload string, want int64) {
	t.Helper()
	if instance.reservedBytes != want {
		t.Errorf("reserved bytes=%d, want %d", instance.reservedBytes, want)
	}
	parts, err := instance.multipartStore.ListParts(context.Background(), upload)
	if err != nil {
		t.Fatal(err)
	}
	var actual int64
	for _, part := range parts {
		actual += part.Size
	}
	if actual != want {
		t.Errorf("actual stored part bytes=%d, want %d", actual, want)
	}
}
