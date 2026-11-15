package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestWorkspaceFileSaveReplayBypassesFreshQuotaAndBasis(t *testing.T) {
	instance, store := openWorkspaceReceiptInstance(t, Options{Backend: BackendWorkspace, MaxBytes: 16, MaxObjects: 1})
	request := workspaceReceiptRequest("save-one", "note", "saved")
	request.Write.Replace = true
	receipt, err := instance.SaveWorkspaceFile(context.Background(), "bucket", request)
	if err != nil || receipt.Outcome != string(storage.WorkspaceFileCommitted) {
		t.Fatalf("initial save receipt=%+v error=%v", receipt, err)
	}
	instance.mu.Lock()
	instance.options.MaxBytes = 1
	instance.mu.Unlock()
	request.Write.Basis = nil
	replayed, found, err := instance.ReplayWorkspaceFileSave(context.Background(), "bucket", request)
	if err != nil || !found || replayed.Outcome != string(storage.WorkspaceFileCommitted) {
		t.Fatalf("replay receipt=%+v found=%v error=%v", replayed, found, err)
	}
	if store == nil {
		t.Fatal("workspace store unexpectedly missing")
	}
}

func TestWorkspaceFileSaveUsesQuotaAndConsumesMultipartReservation(t *testing.T) {
	instance, _ := openWorkspaceReceiptInstance(t, Options{Backend: BackendWorkspace, MaxBytes: 32, MaxObjects: 1, MaxMultipartUploads: 1})
	ctx := context.Background()
	if _, err := instance.CreateMultipartUpload(ctx, "bucket", "note", storage.MultipartOptions{}); err != nil {
		t.Fatal(err)
	}
	create := workspaceReceiptRequest("create-note", "note", "four")
	create.Write.Replace = true
	if receipt, err := instance.SaveWorkspaceFile(ctx, "bucket", create); err != nil || receipt.Outcome != string(storage.WorkspaceFileCommitted) {
		t.Fatalf("create receipt=%+v error=%v", receipt, err)
	}
	assertWorkspaceUsageAndReservation(t, instance, Usage{Bytes: 4, Objects: 1}, objectTarget("bucket", "note"), 0)
	shrink := workspaceReceiptRequest("shrink-note", "note", "xy")
	shrink.Write.Replace = true
	if receipt, err := instance.SaveWorkspaceFile(ctx, "bucket", shrink); err != nil || receipt.Outcome != string(storage.WorkspaceFileCommitted) {
		t.Fatalf("shrink receipt=%+v error=%v", receipt, err)
	}
	assertWorkspaceUsageAndReservation(t, instance, Usage{Bytes: 2, Objects: 1}, objectTarget("bucket", "note"), 0)
}

func TestWorkspaceFileUnknownSaveResolvesAndRestoresReadiness(t *testing.T) {
	base, err := workspace.New(workspace.Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	store := &unknownReceiptStore{Store: base}
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 32, MaxObjects: 2}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	request := workspaceReceiptRequest("uncertain", "note", "new")
	request.Write.Replace = true
	store.forceUnknown = true
	receipt, saveErr := instance.SaveWorkspaceFile(context.Background(), "bucket", request)
	if receipt.Outcome != string(storage.WorkspaceFileUnknown) || !errors.Is(saveErr, storage.ErrSaveRequestUnknown) {
		t.Fatalf("unknown save receipt=%+v error=%v", receipt, saveErr)
	}
	if _, err := instance.PutObject(context.Background(), "bucket", "later", []byte("x"), PutOptions{}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("write while unresolved=%v", err)
	}
	resolved, err := instance.ResolveWorkspaceFileSave(context.Background(), "bucket", "note", request.RequestKey)
	if err != nil || resolved.Outcome != string(storage.WorkspaceFileCommitted) || !resolved.Replayed {
		t.Fatalf("resolve receipt=%+v error=%v", resolved, err)
	}
	if _, err := instance.PutObject(context.Background(), "bucket", "later", []byte("x"), PutOptions{}); err != nil {
		t.Fatalf("write after resolve=%v", err)
	}
}

func TestWorkspaceFileReceiptAuthorizationPrecedesBackendLookup(t *testing.T) {
	base, err := workspace.New(workspace.Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	store := &countingReceiptStore{Store: base}
	granted := authority.All().Without(authority.ObjectWrite)
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, Authority: &granted}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	request := workspaceReceiptRequest("read-only", "note", "value")
	if _, err := instance.ResolveWorkspaceFileSave(context.Background(), "bucket", "note", request.RequestKey); !errors.Is(err, storage.ErrSaveRequestNotFound) {
		t.Fatalf("read-only resolve=%v", err)
	}
	before := store.calls
	if _, err := instance.SaveWorkspaceFile(context.Background(), "bucket", request); err == nil {
		t.Fatal("read-only save unexpectedly succeeded")
	}
	if _, _, err := instance.ReplayWorkspaceFileSave(context.Background(), "bucket", request); err == nil {
		t.Fatal("read-only replay unexpectedly succeeded")
	}
	if store.calls != before {
		t.Fatalf("denied save/replay reached backend: calls %d -> %d", before, store.calls)
	}
	if _, err := instance.ResolveWorkspaceFileSave(context.Background(), "other", "note", request.RequestKey); !errors.Is(err, storage.ErrWorkspaceFileUnsupported) {
		t.Fatalf("wrong bucket resolve=%v", err)
	}
}

func TestWorkspaceFileReceiptPolicyRefusesBeforeLookup(t *testing.T) {
	base, err := workspace.New(workspace.Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	store := &countingReceiptStore{Store: base}
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, Policy: policy.Fixed(&policy.Set{})}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if _, err := instance.ResolveWorkspaceFileSave(context.Background(), "bucket", "note", "key"); err == nil {
		t.Fatal("configured policy unexpectedly allowed workspace receipts")
	}
	if store.calls != 0 {
		t.Fatalf("policy refusal reached backend %d times", store.calls)
	}
}

func openWorkspaceReceiptInstance(t *testing.T, options Options) (*Instance, *workspace.Store) {
	t.Helper()
	store, err := workspace.New(workspace.Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := OpenWithStore(options, store, nil)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	return instance, store
}

func workspaceReceiptRequest(key, path, value string) storage.WorkspaceFileSaveRequest {
	return storage.WorkspaceFileSaveRequest{
		RequestKey: key,
		Write:      storage.WorkspaceFileWriteRequest{Path: path, Data: []byte(value), Replace: true, MaxFileBytes: 64 << 10},
	}
}

type countingReceiptStore struct {
	*workspace.Store
	calls int
}

func (s *countingReceiptStore) ReplayWorkspaceFileSave(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.SaveReceipt, bool, error) {
	s.calls++
	return s.Store.ReplayWorkspaceFileSave(ctx, bucket, request)
}

func (s *countingReceiptStore) SaveWorkspaceFile(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.SaveReceipt, error) {
	s.calls++
	return s.Store.SaveWorkspaceFile(ctx, bucket, request)
}

func (s *countingReceiptStore) ResolveWorkspaceFileSave(ctx context.Context, bucket, path, key string) (storage.SaveReceipt, error) {
	s.calls++
	return s.Store.ResolveWorkspaceFileSave(ctx, bucket, path, key)
}

type unknownReceiptStore struct {
	*workspace.Store
	forceUnknown bool
	blocked      bool
}

func (s *unknownReceiptStore) SaveWorkspaceFile(ctx context.Context, bucket string, request storage.WorkspaceFileSaveRequest) (storage.SaveReceipt, error) {
	receipt, err := s.Store.SaveWorkspaceFile(ctx, bucket, request)
	if s.forceUnknown && err == nil {
		s.blocked = true
		return storage.SaveReceipt{Outcome: string(storage.WorkspaceFileUnknown)}, storage.ErrSaveRequestUnknown
	}
	return receipt, err
}

func (s *unknownReceiptStore) ResolveWorkspaceFileSave(ctx context.Context, bucket, path, key string) (storage.SaveReceipt, error) {
	receipt, err := s.Store.ResolveWorkspaceFileSave(ctx, bucket, path, key)
	if receipt.Outcome == string(storage.WorkspaceFileCommitted) || receipt.Outcome == string(storage.WorkspaceFileNotCommitted) {
		s.blocked = false
	}
	return receipt, err
}

func (s *unknownReceiptStore) UsageRevision(ctx context.Context) (uint64, error) {
	if s.blocked {
		return 1, storage.ErrSaveRequestUnknown
	}
	return s.Store.UsageRevision(ctx)
}
