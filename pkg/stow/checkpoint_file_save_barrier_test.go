package stow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestCheckpointCaptureEntryPointsRefuseCorruptFileSaveMarker(t *testing.T) {
	registryDir := filepath.Join(t.TempDir(), "registry")
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	createCorruptFileSaveMarker(t, ws.Dir())
	if _, err := ws.CreateCheckpoint(context.Background(), CheckpointOptions{}); err == nil {
		t.Fatal("owner capture accepted corrupt file-save marker")
	}
	if _, err := CheckpointOf(context.Background(), registryDir, ws.ID(), CheckpointOptions{}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("external capture=%v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(registryDir, "checkpoints")); err == nil && len(entries) != 0 {
		t.Fatalf("capture published checkpoints before refusal: %v", entries)
	}
}

func TestCheckpointCaptureAllowsTerminalFileSaveMarker(t *testing.T) {
	registryDir := filepath.Join(t.TempDir(), "registry")
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	request := storage.WorkspaceFileSaveRequest{RequestKey: "terminal-capture", Write: storage.WorkspaceFileWriteRequest{
		Path: "note.txt", Data: []byte("saved"), Replace: true, MaxFileBytes: 64 << 10,
	}}
	if _, err := ws.store.SaveWorkspaceFile(context.Background(), ws.Bucket(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateCheckpoint(context.Background(), CheckpointOptions{}); err != nil {
		t.Fatalf("owner capture with terminal journal=%v", err)
	}
	if _, err := CheckpointOf(context.Background(), registryDir, ws.ID(), CheckpointOptions{}); err != nil {
		t.Fatalf("external capture with terminal journal=%v", err)
	}
}

func TestCheckpointCaptureRefusesPendingFileSaveAndKeepsOldSnapshotReadable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	registryDir := filepath.Join(t.TempDir(), "registry")
	ws, editor, request := createCommittedWorkspaceSave(t, root, registryDir)
	old, err := ws.CreateCheckpoint(context.Background(), CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	indexBytes := inducePendingFileSave(t, root, editor, request)
	assertPendingCaptureRefused(t, ws, registryDir, old)
	assertOldCheckpointReadable(t, registryDir, ws.ID(), old)
	closeAndRestorePendingIndex(t, root, ws, editor, indexBytes)
	if _, err := CheckpointOf(context.Background(), registryDir, ws.ID(), CheckpointOptions{}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("external capture after owner close=%v", err)
	}
	assertOnlyCheckpoint(t, registryDir, old.ID)
}

func inducePendingFileSave(t *testing.T, root string, editor *WorkspaceEditor, request workspaceFileSaveRequest) []byte {
	t.Helper()
	if err := editor.ReleaseObservation(request.ObservationToken); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "host-only.txt"), []byte("host bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, ".stow", "index.json")
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexPath, 0o700); err != nil {
		t.Fatal(err)
	}
	read, err := editor.ReadForSave(context.Background(), request.Path)
	if err != nil {
		t.Fatal(err)
	}
	pending := workspaceFileSaveRequest{Path: read.Path, Data: []byte("pending bytes"), RequestKey: "pending-capture", ObservationToken: read.ObservationToken}
	receipt, err := editor.saveFile(context.Background(), pending)
	if !errors.Is(err, storage.ErrSaveRequestUnknown) || receipt.Outcome != string(storage.WorkspaceFileUnknown) {
		t.Fatalf("pending save=%+v, %v", receipt, err)
	}
	return indexBytes
}

func assertPendingCaptureRefused(t *testing.T, ws *Workspace, registryDir string, old CheckpointInfo) {
	t.Helper()
	if _, err := ws.CreateCheckpoint(context.Background(), CheckpointOptions{}); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("owner capture with pending save=%v", err)
	}
	if _, err := CheckpointOf(context.Background(), registryDir, ws.ID(), CheckpointOptions{}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("external capture with pending save=%v", err)
	}
	target := ws.captureTarget()
	receipt := checkpointReceipt{CheckpointID: old.ID, WorkspaceID: ws.ID()}
	target.receipt = &receipt
	request := CheckpointRequest{Hold: &CheckpointHold{ID: "pending-capture-hold", Owner: "owner-example"}}
	if _, err := captureWithRecoveryHold(context.Background(), target, request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("recovery-hold capture with pending save=%v", err)
	}
	if _, err := os.Stat(filepath.Join(registryDir, ".stow", "recovery-holds.json")); !os.IsNotExist(err) {
		t.Fatalf("pending capture admitted a hold: %v", err)
	}
}

func assertOldCheckpointReadable(t *testing.T, registryDir, workspaceID string, checkpoint CheckpointInfo) {
	t.Helper()
	reader := openCheckpointReaderForTest(t, registryDir, workspaceID, checkpoint.ID, nil)
	result, err := reader.ReadFile(context.Background(), "docs/note.txt")
	if err != nil || string(result.Data) != "recover me" {
		t.Fatalf("old immutable checkpoint read=%q, %v", result.Data, err)
	}
}

func assertOnlyCheckpoint(t *testing.T, registryDir, checkpointID string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(registryDir, "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != checkpointID {
		t.Fatalf("checkpoint set after refusals=%v, want only %s", entries, checkpointID)
	}
}

func TestRecoveryHoldCaptureChecksBarrierBeforeAdmission(t *testing.T) {
	registryDir := filepath.Join(t.TempDir(), "registry")
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	createCorruptFileSaveMarker(t, ws.Dir())
	target := ws.captureTarget()
	receipt := checkpointReceipt{CheckpointID: "cp_0123456789abcdef01234567", WorkspaceID: ws.ID()}
	target.receipt = &receipt
	request := CheckpointRequest{Hold: &CheckpointHold{ID: "hold-example", Owner: "owner-example"}}
	if _, err := captureWithRecoveryHold(context.Background(), target, request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("recovery-hold capture=%v", err)
	}
	if _, err := os.Stat(filepath.Join(registryDir, ".stow", "recovery-holds.json")); !os.IsNotExist(err) {
		t.Fatalf("recovery hold was admitted before barrier: %v", err)
	}
}

func createCorruptFileSaveMarker(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".stow", "file-saves"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := workspace.CheckWorkspaceFileSaveBarrier(root, "", ""); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("corrupt marker probe=%v", err)
	}
}
