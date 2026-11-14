package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestWorkspaceFileQuotaRefreshDoesNotAdoptMultipartTarget(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(workspace.Options{Root: root, Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 4, MaxObjects: 2, MaxMultipartUploads: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	ctx := context.Background()
	if _, err := instance.CreateMultipartUpload(ctx, "bucket", "host.txt", storage.MultipartOptions{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "host.txt")
	if err := os.WriteFile(path, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := instance.RefreshUsage(ctx); err != nil {
		t.Fatal(err)
	}
	basis, err := instance.ObserveWorkspaceFile(ctx, "bucket", "host.txt", 64<<10)
	if err != nil || basis.Generation != "" {
		t.Fatalf("unindexed target observation=%+v error=%v", basis, err)
	}
	basis.Data = nil
	publication, err := instance.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "host.txt", Data: []byte("12345"), Basis: &basis, MaxFileBytes: 64 << 10})
	if !errors.Is(err, ErrQuotaExceeded) || publication.Outcome != storage.WorkspaceFileNotCommitted {
		t.Fatalf("over-quota publication=%+v error=%v", publication, err)
	}
	created, err := instance.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "new.txt", Data: []byte("x"), Replace: true, MaxFileBytes: 64 << 10})
	if !errors.Is(err, ErrQuotaExceeded) || created.Outcome != storage.WorkspaceFileNotCommitted {
		t.Fatalf("over-quota create=%+v error=%v", created, err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("over-quota create published a file: %v", err)
	}
	after, observeErr := instance.ObserveWorkspaceFile(ctx, "bucket", "host.txt", 64<<10)
	if observeErr != nil || after.Generation != "" {
		t.Fatalf("quota refusal changed unindexed basis: %+v error=%v", after, observeErr)
	}
}

func TestWorkspaceEscapedPrimaryAndSecondaryOverwriteQuota(t *testing.T) {
	store, err := workspace.New(workspace.Options{Root: t.TempDir(), Bucket: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "secondary"); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("k", 260)
	for _, bucket := range []string{"primary", "secondary"} {
		if _, err := store.PutObject(ctx, bucket, key, bytes.NewReader([]byte("1234")), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 12, MaxObjects: 3}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.PutObject(ctx, "primary", key, []byte("12345678"), PutOptions{}); err != nil {
		t.Fatalf("primary escaped overwrite was charged as a new object: %v", err)
	}
	if _, err := instance.PutObject(ctx, "secondary", key, []byte("abcd"), PutOptions{}); err != nil {
		t.Fatalf("secondary escaped overwrite was charged as a new object: %v", err)
	}
}

func TestWorkspaceFileIndexFailureSurvivesRuntimeRefreshAndBlocksWrites(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(workspace.Options{Root: root, Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.PutObject(ctx, "bucket", "note.txt", bytes.NewReader([]byte("old")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 32, MaxObjects: 2}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	basis, err := instance.ObserveWorkspaceFile(ctx, "bucket", "note.txt", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	basis.Data = nil
	index := filepath.Join(root, ".stow", "index.json")
	backup := index + ".backup"
	if err := os.Rename(index, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0o700); err != nil {
		t.Fatal(err)
	}
	publication, publishErr := instance.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "note.txt", Data: []byte("new"), Basis: &basis, MaxFileBytes: 64 << 10})
	if publication.Outcome != storage.WorkspaceFileCommitted || publication.IndexReady || !errors.Is(publishErr, storage.ErrMutationCommitted) {
		t.Fatalf("publication=%+v error=%v", publication, publishErr)
	}
	if err := instance.RefreshUsage(ctx); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("refresh after committed index failure=%v", err)
	}
	if _, err := instance.PutObject(ctx, "bucket", "later", []byte("x"), PutOptions{}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("new write after committed index failure=%v", err)
	}
	if _, err := instance.ObserveWorkspaceFile(ctx, "bucket", "note.txt", 64<<10); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("read basis after committed index failure=%v", err)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, index); err != nil {
		t.Fatal(err)
	}
}

type unknownWorkspaceSaveStore struct {
	*workspace.Store
	mu      sync.Mutex
	blocked bool
}

func (s *unknownWorkspaceSaveStore) PublishWorkspaceFile(context.Context, string, storage.WorkspaceFileWriteRequest) (storage.WorkspaceFilePublication, error) {
	s.mu.Lock()
	s.blocked = true
	s.mu.Unlock()
	return storage.WorkspaceFilePublication{Outcome: storage.WorkspaceFileUnknown}, storage.ErrSaveRequestUnknown
}

func (s *unknownWorkspaceSaveStore) UsageRevision(ctx context.Context) (uint64, error) {
	s.mu.Lock()
	blocked := s.blocked
	s.mu.Unlock()
	if blocked {
		return 1, storage.ErrSaveRequestUnknown
	}
	return 0, nil
}

func (s *unknownWorkspaceSaveStore) ObserveWorkspaceFile(ctx context.Context, bucket, path string, maxBytes int64) (storage.WorkspaceFileObservation, error) {
	s.mu.Lock()
	blocked := s.blocked
	s.mu.Unlock()
	if blocked {
		return storage.WorkspaceFileObservation{}, storage.ErrWorkspaceFileBlocked
	}
	return s.Store.ObserveWorkspaceFile(ctx, bucket, path, maxBytes)
}

func TestRuntimeUnknownWorkspaceWriteStaysBlockedAcrossRefresh(t *testing.T) {
	root := t.TempDir()
	base, err := workspace.New(workspace.Options{Root: root, Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if _, err := base.PutObject(context.Background(), "bucket", "note", bytes.NewReader([]byte("old")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	store := &unknownWorkspaceSaveStore{Store: base}
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 32, MaxObjects: 2}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	ctx := context.Background()
	basis, err := instance.ObserveWorkspaceFile(ctx, "bucket", "note", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	basis.Data = nil
	publication, err := instance.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "note", Data: []byte("new"), Basis: &basis, MaxFileBytes: 64 << 10})
	if publication.Outcome != storage.WorkspaceFileUnknown || !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("unknown publication=%+v error=%v", publication, err)
	}
	if err := instance.RefreshUsage(ctx); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("refresh after unknown publication=%v", err)
	}
	if _, err := instance.PutObject(ctx, "bucket", "later", []byte("x"), PutOptions{}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("new write after unknown publication=%v", err)
	}
	if _, err := instance.ObserveWorkspaceFile(ctx, "bucket", "note", 64<<10); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("read after unknown publication=%v", err)
	}
}

func TestWorkspaceSaveConsumesMultipartReservationAndAccountsShrink(t *testing.T) {
	store, err := workspace.New(workspace.Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 32, MaxObjects: 1, MaxMultipartUploads: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	ctx := context.Background()
	if _, err := instance.CreateMultipartUpload(ctx, "bucket", "note", storage.MultipartOptions{}); err != nil {
		t.Fatal(err)
	}
	target := objectTarget("bucket", "note")
	assertWorkspaceReservation(t, instance, target, 1)
	publication, err := instance.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "note", Data: []byte("four"), Replace: true, MaxFileBytes: 64 << 10})
	if err != nil || publication.Outcome != storage.WorkspaceFileCommitted {
		t.Fatalf("create against multipart reservation=%+v error=%v", publication, err)
	}
	assertWorkspaceUsageAndReservation(t, instance, Usage{Bytes: 4, Objects: 1}, target, 0)
	basis, err := instance.ObserveWorkspaceFile(ctx, "bucket", "note", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	basis.Data = nil
	publication, err = instance.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "note", Data: []byte("xy"), Basis: &basis, MaxFileBytes: 64 << 10})
	if err != nil || publication.Outcome != storage.WorkspaceFileCommitted {
		t.Fatalf("shrink=%+v error=%v", publication, err)
	}
	assertWorkspaceUsageAndReservation(t, instance, Usage{Bytes: 2, Objects: 1}, target, 0)
	if err := instance.RefreshUsage(ctx); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceUsageAndReservation(t, instance, Usage{Bytes: 2, Objects: 1}, target, 0)
}

func assertWorkspaceReservation(t *testing.T, instance *Instance, target string, want int64) {
	t.Helper()
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.reservedObjects != want || instance.reservedBytes != 0 || !instance.hasTargetReservation(target) {
		t.Fatalf("initial reservations objects=%d bytes=%d target=%v", instance.reservedObjects, instance.reservedBytes, instance.hasTargetReservation(target))
	}
}

func assertWorkspaceUsageAndReservation(t *testing.T, instance *Instance, expected Usage, target string, reserved int64) {
	t.Helper()
	if got := instance.Usage(); got != expected {
		t.Fatalf("usage=%+v, want %+v", got, expected)
	}
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.reservedObjects != reserved || instance.reservedBytes != 0 || instance.hasTargetReservation(target) {
		t.Fatalf("reservations after publication objects=%d bytes=%d target=%v", instance.reservedObjects, instance.reservedBytes, instance.hasTargetReservation(target))
	}
}
