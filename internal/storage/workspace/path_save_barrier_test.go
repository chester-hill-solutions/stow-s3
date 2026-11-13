package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceFileSaveBarrierMissingAndTerminalReady(t *testing.T) {
	if err := CheckWorkspaceFileSaveBarrier(filepath.Join(t.TempDir(), "missing"), "ws-missing", "bucket"); err != nil {
		t.Fatalf("missing root: %v", err)
	}
	store := newFileSaveJournalStore(t)
	if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), store.manifest.Bucket); err != nil {
		t.Fatalf("workspace without marker: %v", err)
	}
	request := newWorkspaceFileSaveRequest(t, store, "terminal-ready", "note", []byte("saved"))
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); err != nil {
		t.Fatal(err)
	}
	if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), store.manifest.Bucket); err != nil {
		t.Fatalf("terminal journal: %v", err)
	}
}

func TestWorkspaceFileSaveBarrierRejectsPendingForeignCorruptAndAliased(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		leavePendingWorkspaceFileSave(t, store)
		if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), store.manifest.Bucket); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("pending barrier=%v", err)
		}
	})
	t.Run("foreign identity", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		request := newWorkspaceFileSaveRequest(t, store, "foreign", "note", []byte("saved"))
		if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); err != nil {
			t.Fatal(err)
		}
		if err := CheckWorkspaceFileSaveBarrier(store.root, "ws-other", "bucket"); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("foreign barrier=%v", err)
		}
	})
	t.Run("missing identity", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		request := newWorkspaceFileSaveRequest(t, store, "missing-identity", "note", []byte("saved"))
		if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(store.root, workspaceFileSaveIdentityPath)); err != nil {
			t.Fatal(err)
		}
		if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), "bucket"); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("missing identity barrier=%v", err)
		}
	})
	t.Run("folded marker", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		if err := os.Mkdir(filepath.Join(store.root, ".stow", "File-Saves"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), "bucket"); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("folded marker barrier=%v", err)
		}
	})
	t.Run("symlinked marker", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		target := t.TempDir()
		marker := filepath.Join(store.root, ".stow", "file-saves")
		if err := os.Symlink(target, marker); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), "bucket"); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("symlinked marker barrier=%v", err)
		}
	})
}

func TestWorkspaceFileSaveBarrierIsReadOnlyAndCloseKeepsPendingEvidence(t *testing.T) {
	store := newFileSaveJournalStore(t)
	leavePendingWorkspaceFileSave(t, store)
	before := digestWorkspaceFileSaveTree(t, store.root)
	if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), "bucket"); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("barrier=%v", err)
	}
	after := digestWorkspaceFileSaveTree(t, store.root)
	if !sameWorkspaceFileSaveTree(before, after) {
		t.Fatal("barrier changed journal evidence")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.root, workspaceFileSavePendingPath)); err != nil {
		t.Fatalf("pending evidence disappeared on close: %v", err)
	}
	if err := CheckWorkspaceFileSaveBarrier(store.root, store.ID(), "bucket"); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("closed-store barrier=%v", err)
	}
}

func TestWorkspaceFileSaveBarrierRefusesDirectDestroy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owned")
	store, err := New(Options{Root: root, Bucket: "bucket", InitiallyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	leavePendingWorkspaceFileSave(t, store)
	if err := store.Destroy(); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("Destroy with pending receipt=%v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("workspace removed despite pending receipt: %v", err)
	}
}

func TestDestroySerializesWithPendingWorkspaceFileSaveAndPreservesResolution(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owned")
	store, err := New(Options{Root: root, Bucket: "bucket", InitiallyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	request := newWorkspaceFileSaveRequest(t, store, "race-save", "note", []byte("saved"))
	entered, release := make(chan struct{}), make(chan struct{})
	store.fileSavePhaseFault = func(phase string) error {
		if phase == workspaceFileSavePrepared {
			close(entered)
			<-release
			return errors.New("interrupt after pending write")
		}
		return nil
	}
	saveDone := make(chan error, 1)
	go func() {
		_, err := store.SaveWorkspaceFile(context.Background(), "bucket", request)
		saveDone <- err
	}()
	<-entered
	destroyDone := make(chan error, 1)
	go func() { destroyDone <- store.Destroy() }()
	close(release)
	saveErr, destroyErr := <-saveDone, <-destroyDone
	if !errors.Is(saveErr, storage.ErrSaveRequestUnknown) || !errors.Is(destroyErr, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("save error=%v destroy error=%v", saveErr, destroyErr)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("Destroy removed root during receipt save: %v", err)
	}
	store.fileSavePhaseFault = nil
	receipt, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note", request.RequestKey)
	if !errors.Is(err, storage.ErrSaveRequestNotCommitted) || receipt.Outcome != workspaceFileSaveNotCommitted {
		t.Fatalf("resolution after refused destroy=%+v error=%v", receipt, err)
	}
}

func leavePendingWorkspaceFileSave(t *testing.T, store *Store) {
	t.Helper()
	request := newWorkspaceFileSaveRequest(t, store, "pending-request", "note", []byte("saved"))
	store.fileSavePhaseFault = func(phase string) error {
		if phase == workspaceFileSavePublishing {
			return errors.New("injected publishing interruption")
		}
		return nil
	}
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("pending save error=%v", err)
	}
	store.fileSavePhaseFault = nil
}

func digestWorkspaceFileSaveTree(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	err := filepath.WalkDir(filepath.Join(root, ".stow", "file-saves"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err == nil {
			files[relative] = sha256.Sum256(data)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sameWorkspaceFileSaveTree(left, right map[string][32]byte) bool {
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

func TestWorkspaceFileSaveBarrierGuardsObjectAndMultipartReadsAndWrites(t *testing.T) {
	store := newFileSaveJournalStore(t)
	if _, err := store.PutObject(context.Background(), "bucket", "note", bytes.NewReader([]byte("old")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBucket(context.Background(), "empty"); err != nil {
		t.Fatal(err)
	}
	upload, err := store.CreateMultipartUpload(context.Background(), "bucket", "staged", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UploadPart(context.Background(), upload.UploadID, 1, bytes.NewReader([]byte("part"))); err != nil {
		t.Fatal(err)
	}
	leavePendingWorkspaceFileSave(t, store)
	before := digestWorkspaceFileSaveTree(t, store.root)
	assertWorkspaceSaveReadBlocked(t, store)
	assertWorkspaceSaveMutationBlocked(t, store, upload.UploadID)
	assertWorkspaceQuotaAndMultipartReads(t, store, upload.UploadID)
	if after := digestWorkspaceFileSaveTree(t, store.root); !sameWorkspaceFileSaveTree(before, after) {
		t.Fatal("blocked object operations changed save evidence")
	}
}

func assertWorkspaceSaveReadBlocked(t *testing.T, store *Store) {
	t.Helper()
	if _, _, err := store.GetObject(context.Background(), "bucket", "note"); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("GetObject while blocked=%v", err)
	}
	if _, err := store.HeadObject(context.Background(), "bucket", "note"); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("HeadObject while blocked=%v", err)
	}
	if _, err := store.ListObjectsV2(context.Background(), "bucket", storage.ListOptions{}); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("ListObjects while blocked=%v", err)
	}
	if _, err := store.ObserveWorkspaceFile(context.Background(), "bucket", "note", 64<<10); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("workspace file observation while blocked=%v", err)
	}
}

func assertWorkspaceSaveMutationBlocked(t *testing.T, store *Store, uploadID string) {
	t.Helper()
	if _, err := store.PutObject(context.Background(), "bucket", "note", bytes.NewReader([]byte("new")), storage.PutOptions{}); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("PutObject while blocked=%v", err)
	}
	if err := store.DeleteObject(context.Background(), "bucket", "note"); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("DeleteObject while blocked=%v", err)
	}
	if _, err := store.CopyObject(context.Background(), "bucket", "note", "bucket", "copy"); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("CopyObject while blocked=%v", err)
	}
	assertWorkspaceSaveBucketMutationBlocked(t, store)
	assertWorkspaceSaveMultipartMutationBlocked(t, store, uploadID)
}

func assertWorkspaceSaveBucketMutationBlocked(t *testing.T, store *Store) {
	t.Helper()
	if err := store.CreateBucket(context.Background(), "new"); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("CreateBucket while blocked=%v", err)
	}
	if err := store.DeleteBucket(context.Background(), "empty"); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("DeleteBucket while blocked=%v", err)
	}
}

func assertWorkspaceSaveMultipartMutationBlocked(t *testing.T, store *Store, uploadID string) {
	t.Helper()
	if _, err := store.CreateMultipartUpload(context.Background(), "bucket", "new", storage.MultipartOptions{}); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("CreateMultipartUpload while blocked=%v", err)
	}
	if _, err := store.UploadPart(context.Background(), uploadID, 2, bytes.NewReader([]byte("x"))); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("UploadPart while blocked=%v", err)
	}
	if _, err := store.CompleteMultipartUpload(context.Background(), uploadID, []storage.PartInfo{{PartNumber: 1}}); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("CompleteMultipartUpload while blocked=%v", err)
	}
	if err := store.AbortMultipartUpload(context.Background(), uploadID); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("AbortMultipartUpload while blocked=%v", err)
	}
}

func assertWorkspaceQuotaAndMultipartReads(t *testing.T, store *Store, uploadID string) {
	t.Helper()
	quota := store.QuotaStore()
	if _, err := quota.HeadObject(context.Background(), "bucket", "note"); err != nil {
		t.Fatalf("quota HeadObject while blocked=%v", err)
	}
	if _, err := quota.ListObjectsV2(context.Background(), "bucket", storage.ListOptions{}); err != nil {
		t.Fatalf("quota ListObjects while blocked=%v", err)
	}
	if _, err := store.GetMultipartUpload(context.Background(), uploadID); err != nil {
		t.Fatalf("GetMultipartUpload while blocked=%v", err)
	}
	if _, err := store.ListMultipartUploads(context.Background(), "bucket", storage.MultipartListOptions{}); err != nil {
		t.Fatalf("ListMultipartUploads while blocked=%v", err)
	}
	if parts, err := store.ListParts(context.Background(), uploadID); err != nil || len(parts) != 1 {
		t.Fatalf("ListParts while blocked=%v parts=%d", err, len(parts))
	}
}

func TestWorkspaceFileSaveBarrierBlocksRegistryCleanupUntilTerminal(t *testing.T) {
	requireAttachmentLocks(t)
	registryDir := filepath.Join(t.TempDir(), "registry")
	registry, err := OpenRegistry(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "workspace")
	store, err := New(Options{Root: root, Bucket: "bucket", InitiallyOwned: true, TTLSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	id := store.ID()
	old := time.Now().Add(-2 * time.Hour)
	if err := registry.Register(Entry{ID: id, Dir: root, Bucket: "bucket", Owned: true, TTLSeconds: 1, Created: old, LastUsed: old}); err != nil {
		t.Fatal(err)
	}
	leavePendingWorkspaceFileSave(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	checkpointID := "cp_0123456789abcdef01234567"
	checkpointDir := filepath.Join(registryDir, "checkpoints", checkpointID)
	if err := os.MkdirAll(checkpointDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(checkpointReference{ID: checkpointID, WorkspaceID: id})
	if err := os.WriteFile(filepath.Join(checkpointDir, "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	assertRegistryCleanupRefused(t, registry, id, checkpointDir)
	if _, found, err := registry.Lookup(id); err != nil || !found {
		t.Fatalf("registry identity after refusals: found=%v error=%v", found, err)
	}
	if err := CheckWorkspaceFileSaveBarrier(root, id, "bucket"); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("pending marker after close=%v", err)
	}
}

func assertRegistryCleanupRefused(t *testing.T, registry *Registry, id, checkpointDir string) {
	t.Helper()
	called := false
	if err := registry.RemoveWorkspace(id, func() error { called = true; return nil }); !errors.Is(err, storage.ErrSaveRequestUnknown) || called {
		t.Fatalf("RemoveWorkspace error=%v callback=%v", err, called)
	}
	if err := registry.Forget(id); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("Forget pending workspace=%v", err)
	}
	if err := registry.ForgetCheckpoints(id); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("ForgetCheckpoints pending workspace=%v", err)
	}
	if _, err := os.Stat(checkpointDir); err != nil {
		t.Fatalf("checkpoint removed despite barrier: %v", err)
	}
	results, err := registry.Collect(time.Now().Add(time.Hour))
	if err != nil || len(results) != 1 || !strings.Contains(results[0].Reason, "unreadable") {
		t.Fatalf("Collect result=%+v error=%v", results, err)
	}
}

func TestWorkspaceFileSaveBarrierAllowsTerminalRegistryCleanup(t *testing.T) {
	requireAttachmentLocks(t)
	registryDir := filepath.Join(t.TempDir(), "registry")
	registry, err := OpenRegistry(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "workspace")
	store, err := New(Options{Root: root, Bucket: "bucket", InitiallyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	id := store.ID()
	if err := registry.Register(Entry{ID: id, Dir: root, Bucket: "bucket", Owned: true}); err != nil {
		t.Fatal(err)
	}
	request := newWorkspaceFileSaveRequest(t, store, "terminal-cleanup", "note", []byte("done"))
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := registry.RemoveWorkspace(id, func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("terminal RemoveWorkspace callback=%v error=%v", called, err)
	}
	if _, found, err := registry.Lookup(id); err != nil || found {
		t.Fatalf("terminal workspace registry found=%v error=%v", found, err)
	}
}
