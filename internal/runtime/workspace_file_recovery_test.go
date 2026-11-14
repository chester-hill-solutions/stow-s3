package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestWorkspaceReceiptRecoveryOpensOverQuotaWithoutWideningWrites(t *testing.T) {
	store := newReceiptRecoveryWorkspace(t)
	request := workspaceReceiptRequest("recover-overquota", "existing", "12345678")
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(context.Background(), "bucket", "other", bytes.NewReader([]byte("x")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	options := Options{Backend: BackendWorkspace, MaxBytes: 4, MaxObjects: 1}
	if _, err := OpenWithStore(options, store, nil); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("ordinary open over quota=%v", err)
	}
	instance, err := OpenWithStoreForWorkspaceFileReceiptResolution(options, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if got := instance.Usage(); got.Bytes != 9 || got.Objects != 2 || instance.limits.MaxBytes != 4 || instance.limits.MaxObjects != 1 {
		t.Fatalf("recovery state=%+v limits=%+v", got, instance.limits)
	}
	if receipt, err := instance.ResolveWorkspaceFileSave(context.Background(), "bucket", "existing", request.RequestKey); err != nil || receipt.Outcome != string(storage.WorkspaceFileCommitted) {
		t.Fatalf("resolve over-quota receipt=%+v error=%v", receipt, err)
	}
	if _, err := instance.PutObject(context.Background(), "bucket", "extra", []byte("x"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("fresh over-quota write=%v", err)
	}
}

func TestWorkspaceReceiptRecoveryResolvesUnknownWithReadAuthorityWithoutStoreWrites(t *testing.T) {
	base := newReceiptRecoveryWorkspace(t)
	request := workspaceReceiptRequest("recover-one", "note", "committed")
	if _, err := base.SaveWorkspaceFile(context.Background(), "bucket", request); err != nil {
		t.Fatal(err)
	}
	before := digestWorkspaceInternal(t, base.Root())
	payloadBefore, err := os.ReadFile(filepath.Join(base.Root(), "note"))
	if err != nil {
		t.Fatal(err)
	}
	store := &receiptRecoveryUnknownStore{Store: base, pending: true}
	granted := authority.All().Without(authority.ObjectWrite)
	instance, err := OpenWithStoreForWorkspaceFileReceiptResolution(Options{Backend: BackendWorkspace, Authority: &granted}, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if !errors.Is(instance.recoveryErr, storage.ErrSaveRequestUnknown) {
		t.Fatalf("initial recovery state=%v", instance.recoveryErr)
	}
	if after := digestWorkspaceInternal(t, base.Root()); !sameWorkspaceDigests(before, after) {
		t.Fatal("runtime initialization changed workspace metadata or receipt journal")
	}
	if payloadAfter, err := os.ReadFile(filepath.Join(base.Root(), "note")); err != nil || !bytes.Equal(payloadBefore, payloadAfter) {
		t.Fatalf("runtime initialization changed receipt payload: %v", err)
	}
	receipt, err := instance.ResolveWorkspaceFileSave(context.Background(), "bucket", "note", request.RequestKey)
	if err != nil || receipt.Outcome != string(storage.WorkspaceFileCommitted) || !store.resolved {
		t.Fatalf("read-only resolve=%+v resolved=%v error=%v", receipt, store.resolved, err)
	}
	if _, err := instance.PutObject(context.Background(), "bucket", "denied", []byte("x"), PutOptions{}); err == nil {
		t.Fatal("recovery instance wrote without ObjectWrite authority")
	}
}

func TestWorkspaceReceiptRecoveryTracksAndCleansOverLimitMultipartUploads(t *testing.T) {
	store := newReceiptRecoveryWorkspace(t)
	for _, key := range []string{"one", "two", "three"} {
		if _, err := store.CreateMultipartUpload(context.Background(), "bucket", key, storage.MultipartOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	options := Options{Backend: BackendWorkspace, MaxMultipartUploads: 1}
	if _, err := OpenWithStore(options, store, nil); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("ordinary open over multipart limit=%v", err)
	}
	instance, err := OpenWithStoreForWorkspaceFileReceiptResolution(options, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if len(instance.multipart) != 3 || instance.limits.MaxMultipartUploads != 1 {
		t.Fatalf("recovery tracked %d uploads with limit %d", len(instance.multipart), instance.limits.MaxMultipartUploads)
	}
	for id := range instance.multipart {
		if err := instance.AbortMultipartUpload(context.Background(), id); err != nil {
			t.Fatalf("abort %q: %v", id, err)
		}
	}
	if _, err := instance.CreateMultipartUpload(context.Background(), "bucket", "new", storage.MultipartOptions{}); err != nil {
		t.Fatalf("new upload after cleanup: %v", err)
	}
	if _, err := instance.CreateMultipartUpload(context.Background(), "bucket", "second", storage.MultipartOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("new upload exceeded original cap: %v", err)
	}
}

func TestWorkspaceReceiptRecoveryRejectsUnsafeModesBeforeScanning(t *testing.T) {
	ctxStore := newReceiptRecoveryWorkspace(t)
	if _, err := OpenWithStoreForWorkspaceFileReceiptResolution(Options{Backend: BackendMemory}, storage.NewMemoryStore()); !errors.Is(err, storage.ErrWorkspaceFileUnsupported) {
		t.Fatalf("wrong backend=%v", err)
	}
	if _, err := OpenWithStoreForWorkspaceFileReceiptResolution(Options{Backend: BackendWorkspace, Policy: policy.Fixed(&policy.Set{})}, ctxStore); !errors.Is(err, storage.ErrWorkspaceFileUnsupported) {
		t.Fatalf("policy profile=%v", err)
	}
	denied := authority.None()
	if _, err := OpenWithStoreForWorkspaceFileReceiptResolution(Options{Backend: BackendWorkspace, Authority: &denied}, ctxStore); err == nil {
		t.Fatal("runtime scanned without ObjectRead authority")
	}
}

func newReceiptRecoveryWorkspace(t *testing.T) *workspace.Store {
	t.Helper()
	store, err := workspace.New(workspace.Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func digestWorkspaceInternal(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	digests := make(map[string][32]byte)
	err := filepath.WalkDir(filepath.Join(root, ".stow"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		digests[relative] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return digests
}

func sameWorkspaceDigests(left, right map[string][32]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for path, digest := range left {
		if right[path] != digest {
			return false
		}
	}
	return true
}

type receiptRecoveryUnknownStore struct {
	*workspace.Store
	pending  bool
	resolved bool
}

func (s *receiptRecoveryUnknownStore) UsageRevision(ctx context.Context) (uint64, error) {
	if s.pending {
		return 7, storage.ErrSaveRequestUnknown
	}
	return s.Store.UsageRevision(ctx)
}

func (s *receiptRecoveryUnknownStore) ResolveWorkspaceFileSave(ctx context.Context, bucket, path, requestKey string) (storage.SaveReceipt, error) {
	receipt, err := s.Store.ResolveWorkspaceFileSave(ctx, bucket, path, requestKey)
	if receipt.Outcome == string(storage.WorkspaceFileCommitted) || receipt.Outcome == string(storage.WorkspaceFileNotCommitted) {
		s.pending, s.resolved = false, true
	}
	return receipt, err
}
