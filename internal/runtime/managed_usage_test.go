package runtime

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type managedUsageStore struct {
	*fs.FilesystemStore
	scans     int
	loseReply bool
}

func (s *managedUsageStore) ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	s.scans++
	return s.FilesystemStore.ListObjectsV2(ctx, bucket, opts)
}
func (s *managedUsageStore) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	meta, err := s.FilesystemStore.PutObject(ctx, bucket, key, body, opts)
	if err == nil && s.loseReply {
		return nil, storage.ErrSaveRequestUnknown
	}
	return meta, err
}

func (s *managedUsageStore) StageObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (storage.StagedObject, error) {
	pending, err := s.FilesystemStore.StageObject(ctx, bucket, key, body, opts)
	return &lostReplyStage{StagedObject: pending, store: s}, err
}

type lostReplyStage struct {
	storage.StagedObject
	store *managedUsageStore
}

func (p *lostReplyStage) Commit(ctx context.Context) (*storage.ObjectMeta, error) {
	meta, err := p.StagedObject.Commit(ctx)
	if err == nil && p.store.loseReply {
		return nil, storage.ErrSaveRequestUnknown
	}
	return meta, err
}
func managedUsageInstance(t *testing.T) (*Instance, *managedUsageStore) {
	t.Helper()
	backing, err := fs.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !backing.SupportsDurableSaveRequests() {
		_ = backing.Close()
		t.Skip("native durable backend unavailable")
	}
	store := &managedUsageStore{FilesystemStore: backing}
	if err := store.CreateBucket(context.Background(), "assets"); err != nil {
		t.Fatal(err)
	}
	instance, err := OpenWithStore(Options{Backend: BackendFilesystem, MaxBytes: 5, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	return instance, store
}
func TestManagedFilesystemAccountsSuccessfulMutationsWithoutRescanning(t *testing.T) {
	instance, store := managedUsageInstance(t)
	ctx := context.Background()
	initial := store.scans
	for _, body := range []string{"one", "four"} {
		if _, err := instance.PutObject(ctx, "assets", "key", []byte(body), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := instance.DeleteObject(ctx, "assets", "key"); err != nil {
		t.Fatal(err)
	}
	if instance.Usage() != (Usage{}) || store.scans != initial {
		t.Fatalf("usage=%+v scans=%d initial=%d", instance.Usage(), store.scans, initial)
	}
	if err := instance.RefreshUsage(ctx); err != nil {
		t.Fatal(err)
	}
	if store.scans != initial+1 {
		t.Fatal("explicit refresh did not rescan")
	}
}
func TestManagedFilesystemDetectsMutationsOutsideRuntime(t *testing.T) {
	instance, store := managedUsageInstance(t)
	ctx := context.Background()
	if _, err := store.FilesystemStore.PutObject(ctx, "assets", "outside", strings.NewReader("saved"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "assets", "extra", []byte("x"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("outside write bypassed quota: %v", err)
	}
	if instance.Usage() != (Usage{Bytes: 5, Objects: 1}) {
		t.Fatalf("usage=%+v", instance.Usage())
	}
}
func TestManagedFilesystemReconcilesCommittedWriteWithLostReply(t *testing.T) {
	instance, store := managedUsageInstance(t)
	ctx := context.Background()
	initial := store.scans
	store.loseReply = true
	if _, err := instance.PutObject(ctx, "assets", "key", []byte("saved"), PutOptions{}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("lost reply: %v", err)
	}
	store.loseReply = false
	if _, err := instance.PutObject(ctx, "assets", "extra", []byte("x"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("uncertain write bypassed quota: %v", err)
	}
	if store.scans != initial+1 || instance.Usage() != (Usage{Bytes: 5, Objects: 1}) {
		t.Fatalf("scans=%d usage=%+v", store.scans, instance.Usage())
	}
	if err := instance.DeleteObject(ctx, "assets", "key"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "assets", "extra", []byte("x"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
}

func (s *managedUsageStore) ScanObjectUsage(ctx context.Context, bucket string) (storage.ObjectUsage, error) {
	s.scans++
	return s.FilesystemStore.ScanObjectUsage(ctx, bucket)
}
