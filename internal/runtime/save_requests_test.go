package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type lostSaveReplyStore struct {
	*storage.MemoryStore
	receipt storage.SaveReceipt
}

func (*lostSaveReplyStore) SupportsDurableSaveRequests() bool { return true }
func (s *lostSaveReplyStore) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	meta, err := s.MemoryStore.PutObject(ctx, bucket, key, body, opts)
	if err != nil {
		return meta, err
	}
	if opts.RequestKey != "" {
		s.receipt = storage.SaveReceipt{Meta: meta, Outcome: "committed"}
		return nil, errors.Join(storage.ErrSaveRequestUnknown, storage.ErrSaveRequestFull)
	}
	return meta, nil
}
func (s *lostSaveReplyStore) ReplaySaveRequest(_ context.Context, _, _ string, _ []byte, _ storage.PutOptions) (storage.SaveReceipt, bool, error) {
	receipt := s.receipt
	receipt.Replayed = receipt.Meta != nil
	return receipt, receipt.Meta != nil, nil
}
func (s *lostSaveReplyStore) ResolveSaveRequest(_ context.Context, _, _, _ string) (storage.SaveReceipt, error) {
	return s.receipt, nil
}

func TestDurableSaveLostReplyReconcilesEffectAndAccounting(t *testing.T) {
	ctx := context.Background()
	store := &lostSaveReplyStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{Backend: BackendFilesystem, MaxBytes: 5, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	_, condition, err := instance.ReadForSave(ctx, "guarded", "report")
	if err != nil {
		t.Fatal(err)
	}
	opts := SaveOptions{Condition: condition, RequestKey: "lost-reply"}
	result, err := instance.SaveObject(ctx, "guarded", "report", []byte("saved"), opts)
	if !errors.Is(err, storage.ErrSaveRequestUnknown) || result.Outcome != SaveUnknown {
		t.Fatalf("lost reply=%+v %v", result, err)
	}
	replay, err := instance.SaveObject(ctx, "guarded", "report", []byte("saved"), opts)
	assertResolvedCommit(t, replay, err)
	if !replay.Replayed {
		t.Fatal("receipt was not replayed")
	}
	resolved, err := instance.ResolveSave(ctx, "guarded", "report", "lost-reply")
	if err != nil || resolved.Outcome != SaveCommitted || resolved.Object.Size != 5 || instance.Usage() != (Usage{Bytes: 5, Objects: 1}) {
		t.Fatalf("resolve=%+v %v usage=%+v", resolved, err, instance.Usage())
	}
	if _, err := instance.PutObject(ctx, "guarded", "extra", []byte("x"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("lost effect bypassed quota: %v", err)
	}
	if _, err := instance.PutObject(ctx, "guarded", "report", []byte("edit"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if instance.Usage() != (Usage{Bytes: 4, Objects: 1}) {
		t.Fatalf("replacement double-accounted: %+v", instance.Usage())
	}
}

func TestDurableSaveUnknownReplyFollowedByMutationRefreshesUsage(t *testing.T) {
	ctx := context.Background()
	store := &lostSaveReplyStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{Backend: BackendFilesystem, MaxBytes: 5, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	_, _ = instance.SaveObject(ctx, "guarded", "report", []byte("saved"), SaveOptions{Condition: ReplacementCondition(), RequestKey: "lost"})
	if _, err := instance.PutObject(ctx, "guarded", "extra", []byte("x"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("unresolved save bypassed admission: %v", err)
	}
	if instance.Usage() != (Usage{Bytes: 5, Objects: 1}) {
		t.Fatalf("usage=%+v", instance.Usage())
	}
}

func TestDurableSaveResolutionKeepsCurrentObjectsAndMultipartReservations(t *testing.T) {
	ctx := context.Background()
	store, err := fs.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !store.SupportsDurableSaveRequests() {
		_ = store.Close()
		t.Skip("durable filesystem requests unsupported on this platform")
	}
	instance, err := OpenWithStore(Options{Backend: BackendFilesystem, MaxBytes: 20, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	saved, err := instance.SaveObject(ctx, "guarded", "same", []byte("original"), SaveOptions{Condition: ReplacementCondition(), RequestKey: "retained"})
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.DeleteObject(ctx, "guarded", "same"); err != nil {
		t.Fatal(err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "guarded", "same", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.UploadPart(ctx, upload.UploadID, 1, bytes.NewBufferString("part")); err != nil {
		t.Fatal(err)
	}
	resolved, err := instance.ResolveSave(ctx, "guarded", "same", "retained")
	if err != nil || resolved.Object.Size != saved.Object.Size || instance.Usage() != (Usage{}) || instance.reservedBytes != 4 || instance.reservedObjects != 1 {
		t.Fatalf("resolve=%+v err=%v usage=%+v reserve=%d/%d", resolved, err, instance.Usage(), instance.reservedBytes, instance.reservedObjects)
	}
	if _, err := instance.SaveObject(ctx, "guarded", "elsewhere", []byte("x"), SaveOptions{Condition: ReplacementCondition()}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("resolution lost multipart reservation: %v", err)
	}
}

func assertResolvedCommit(t *testing.T, result SaveResult, err error) {
	t.Helper()
	if err != nil || result.Outcome != SaveCommitted {
		t.Fatalf("receipt=%+v err=%v", result, err)
	}
}
