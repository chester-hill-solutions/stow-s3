package stow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestResumeWorkspaceForFileRecoveryResolvesPendingWithoutRewritingPayload(t *testing.T) {
	root, registryDir := recoveryWorkspacePaths(t)
	owner, editor, request, indexBytes := createPendingWorkspaceSave(t, root, registryDir)
	closeAndRestorePendingIndex(t, root, owner, editor, indexBytes)
	before := recoverySnapshot(t, root, registryDir, owner.ID())
	recovered := openReadOnlyRecoveryOwner(t, registryDir, owner.ID(), root, before)
	defer recovered.Close()
	assertPendingRecoveryResolution(t, recovered, root, request)
}

func closeAndRestorePendingIndex(t *testing.T, root string, owner *Workspace, editor *WorkspaceEditor, indexBytes []byte) {
	t.Helper()
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, ".stow", "index.json")
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, indexBytes, 0o600); err != nil {
		t.Fatal(err)
	}
}

func openReadOnlyRecoveryOwner(t *testing.T, registryDir, id, root string, before map[string][32]byte) *Workspace {
	t.Helper()
	if _, err := ResumeWorkspaceForFileRecovery(WorkspaceOptions{RegistryDir: registryDir, MaxMultipartUploads: 3}, id); err == nil {
		t.Fatal("recovery widened the registered multipart cap")
	}
	assertRecoverySnapshot(t, root, registryDir, id, before)
	none := AllowNone()
	_, err := ResumeWorkspaceForFileRecovery(WorkspaceOptions{RegistryDir: registryDir, Authority: &none}, id)
	var denied *ErrNotAuthorized
	if !errors.As(err, &denied) {
		t.Fatalf("recovery without read authority = %v", err)
	}
	readOnly := ReadOnly()
	recovered, err := ResumeWorkspaceForFileRecovery(WorkspaceOptions{RegistryDir: registryDir, Authority: &readOnly}, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := recovered.Runtime.Capabilities().MaxMultipartUploads; got != 2 {
		t.Fatalf("resumed multipart limit = %d, want 2", got)
	}
	if !recovered.Runtime.Authority().Allows(ObjectRead) || recovered.Runtime.Authority().Allows(ObjectWrite) {
		t.Fatal("recovery did not retain the read-only authority")
	}
	assertRecoverySnapshot(t, root, registryDir, id, before)
	return recovered
}

func assertPendingRecoveryResolution(t *testing.T, recovered *Workspace, root string, request workspaceFileSaveRequest) {
	t.Helper()
	recoveryEditor := openPrivateEditor(t, recovered, []string{"."}, []string{"docs"})
	defer recoveryEditor.Close()
	if _, err := recoveryEditor.ReadForSave(context.Background(), request.Path); !errors.Is(err, ErrWorkspaceEditorUnavailable) {
		t.Fatalf("observation while paused = %v", err)
	}
	publishedInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(request.Path)))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := recoveryEditor.resolveSave(context.Background(), request.Path, request.RequestKey)
	if err != nil || resolved.Outcome != string(storage.WorkspaceFileCommitted) || resolved.Meta == nil {
		t.Fatalf("recovery resolution = %+v, %v", resolved, err)
	}
	assertPayloadIdentity(t, root, request, publishedInfo)
	if _, err := recoveryEditor.ReadForSave(context.Background(), request.Path); err != nil {
		t.Fatalf("observation after resolution = %v", err)
	}
}

func TestResumeWorkspaceForFileRecoveryRefusesOrdinaryOpenAndForeignState(t *testing.T) {
	root, registryDir := recoveryWorkspacePaths(t)
	owner, editor, request := createCommittedWorkspaceSave(t, root, registryDir)
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	before := recoverySnapshot(t, root, registryDir, owner.ID())
	if _, err := ResumeWith(WorkspaceOptions{RegistryDir: registryDir}, owner.ID()); !errors.Is(err, storage.ErrWorkspaceFileUnsupported) {
		t.Fatalf("ordinary resume with journal = %v", err)
	}
	assertRecoverySnapshot(t, root, registryDir, owner.ID(), before)
	if _, err := ResumeWorkspaceForFileRecovery(WorkspaceOptions{RegistryDir: registryDir}, "ws_ffffffffffffffff"); err == nil {
		t.Fatal("foreign workspace identity was accepted")
	}
	assertRecoverySnapshot(t, root, registryDir, owner.ID(), before)
	if err := os.WriteFile(filepath.Join(root, ".stow", "file-saves", "identity.json"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt := recoverySnapshot(t, root, registryDir, owner.ID())
	if _, err := ResumeWorkspaceForFileRecovery(WorkspaceOptions{RegistryDir: registryDir}, owner.ID()); !errors.Is(err, ErrSaveRequestUnknown) {
		t.Fatalf("corrupt journal resume = %v", err)
	}
	assertRecoverySnapshot(t, root, registryDir, owner.ID(), corrupt)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(request.Path))); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceOwnerClaimsSessionBeforeStoreInitialization(t *testing.T) {
	root, registryDir := recoveryWorkspacePaths(t)
	owner, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	before := recoverySnapshot(t, root, registryDir, owner.ID())
	if _, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: registryDir}); !errors.Is(err, workspace.ErrWorkspaceInUse) {
		t.Fatalf("second owner open = %v", err)
	}
	assertRecoverySnapshot(t, root, registryDir, owner.ID(), before)
}

func TestRegisteredResumeDoesNotRecreateMissingRootOrMetadata(t *testing.T) {
	root, registryDir := recoveryWorkspacePaths(t)
	makeEditorWorkspace(t, root)
	owner, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	id := owner.ID()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	assertRegisteredResumeRefuses(t, registryDir, id)
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("resume recreated root: %v", err)
	}
	secondRoot := filepath.Join(t.TempDir(), "work")
	makeEditorWorkspace(t, secondRoot)
	second, err := OpenWorkspace(WorkspaceOptions{Dir: secondRoot, RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	secondID := second.ID()
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(secondRoot, ".stow")); err != nil {
		t.Fatal(err)
	}
	assertRegisteredResumeRefuses(t, registryDir, secondID)
	if _, err := os.Lstat(filepath.Join(secondRoot, ".stow")); !os.IsNotExist(err) {
		t.Fatalf("resume recreated metadata directory: %v", err)
	}
}

func assertRegisteredResumeRefuses(t *testing.T, registryDir, id string) {
	t.Helper()
	if _, err := ResumeWith(WorkspaceOptions{RegistryDir: registryDir}, id); err == nil {
		t.Fatal("ordinary resume accepted missing owner state")
	}
	if _, err := ResumeWorkspaceForFileRecovery(WorkspaceOptions{RegistryDir: registryDir}, id); err == nil {
		t.Fatal("recovery resume accepted missing owner state")
	}
}

func TestResumeWorkspaceForFileRecoveryRetainsRegisteredReadOnlyAuthority(t *testing.T) {
	root, registryDir := recoveryWorkspacePaths(t)
	makeEditorWorkspace(t, root)
	readOnly := ReadOnly()
	owner, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: registryDir, Authority: &readOnly})
	if err != nil {
		t.Fatal(err)
	}
	storeRequest := storage.WorkspaceFileSaveRequest{RequestKey: "read-only-receipt",
		Write: storage.WorkspaceFileWriteRequest{Path: "docs/note.txt", Data: []byte("saved"), Replace: true, MaxFileBytes: 64 << 10}}
	receipt, err := owner.store.SaveWorkspaceFile(context.Background(), owner.Bucket(), storeRequest)
	if err != nil || receipt.Outcome != string(storage.WorkspaceFileCommitted) {
		t.Fatalf("seed receipt = %+v, %v", receipt, err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := ResumeWorkspaceForFileRecovery(WorkspaceOptions{RegistryDir: registryDir}, owner.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if !recovered.Runtime.Authority().Allows(ObjectRead) || recovered.Runtime.Authority().Allows(ObjectWrite) {
		t.Fatal("recovery widened the persisted read-only authority")
	}
	editor := openPrivateEditor(t, recovered, []string{"docs"}, []string{"docs"})
	defer editor.Close()
	resolved, err := editor.resolveSave(context.Background(), storeRequest.Write.Path, storeRequest.RequestKey)
	if err != nil || !resolved.Replayed {
		t.Fatalf("read-only receipt resolution = %+v, %v", resolved, err)
	}
}

func TestResumeMultipartLimitPreservesAndNarrowsPersistedValue(t *testing.T) {
	for _, test := range []struct {
		stored, requested, want int64
		refuse                  bool
	}{
		{stored: 3, want: 3}, {stored: 3, requested: 1, want: 1}, {stored: 3, requested: 4, refuse: true},
		{requested: 0, want: 1}, {requested: 1, want: 1}, {requested: 2, refuse: true},
	} {
		got, err := resumeMultipartLimit(test.stored, test.requested)
		if test.refuse && err == nil {
			t.Fatalf("resume cap (%d,%d) unexpectedly accepted: %d", test.stored, test.requested, got)
		}
		if !test.refuse && (err != nil || got != test.want) {
			t.Fatalf("resume cap (%d,%d) = %d, %v; want %d", test.stored, test.requested, got, err, test.want)
		}
	}
}

func TestResumeWithRetainsNewWorkspaceMultipartLimit(t *testing.T) {
	root, registryDir := recoveryWorkspacePaths(t)
	makeEditorWorkspace(t, root)
	owner, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: registryDir, MaxMultipartUploads: 2})
	if err != nil {
		t.Fatal(err)
	}
	id := owner.ID()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := ResumeWith(WorkspaceOptions{RegistryDir: registryDir}, id)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if got := resumed.Runtime.Capabilities().MaxMultipartUploads; got != 2 {
		t.Fatalf("resumed multipart limit = %d, want 2", got)
	}
}

func TestResumeWithLegacyMultipartCapUsesRuntimeDefault(t *testing.T) {
	root, registryDir := recoveryWorkspacePaths(t)
	makeEditorWorkspace(t, root)
	owner, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	id := owner.ID()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	registry, err := workspace.OpenRegistry(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, found, err := registry.Lookup(id)
	if err != nil || !found {
		t.Fatalf("registry lookup found=%v err=%v", found, err)
	}
	entry.MaxMultipartUploads = 0
	if err := registry.Register(entry); err != nil {
		t.Fatal(err)
	}
	resumed, err := ResumeWith(WorkspaceOptions{RegistryDir: registryDir}, id)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if got := resumed.Runtime.Capabilities().MaxMultipartUploads; got != stowruntime.DefaultMaxMultipartUploads {
		t.Fatalf("legacy multipart cap = %d, want default %d", got, stowruntime.DefaultMaxMultipartUploads)
	}
}

func createCommittedWorkspaceSave(t *testing.T, root, registry string) (*Workspace, *WorkspaceEditor, workspaceFileSaveRequest) {
	t.Helper()
	makeEditorWorkspace(t, root)
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: registry, MaxMultipartUploads: 2})
	if err != nil {
		t.Fatal(err)
	}
	editor := openPrivateEditor(t, ws, []string{"."}, []string{"docs"})
	read, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	request := workspaceFileSaveRequest{Path: read.Path, Data: []byte("recover me"), RequestKey: "recovery-request", ObservationToken: read.ObservationToken}
	receipt, err := editor.saveFile(context.Background(), request)
	if err != nil || receipt.Outcome != string(storage.WorkspaceFileCommitted) {
		t.Fatalf("save = %+v, %v", receipt, err)
	}
	return ws, editor, request
}

func createPendingWorkspaceSave(t *testing.T, root, registry string) (*Workspace, *WorkspaceEditor, workspaceFileSaveRequest, []byte) {
	t.Helper()
	ws, editor, request := createCommittedWorkspaceSave(t, root, registry)
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
	request = workspaceFileSaveRequest{Path: read.Path, Data: []byte("pending bytes"), RequestKey: "pending-request", ObservationToken: read.ObservationToken}
	receipt, err := editor.saveFile(context.Background(), request)
	if !errors.Is(err, storage.ErrSaveRequestUnknown) || receipt.Outcome != string(storage.WorkspaceFileUnknown) {
		t.Fatalf("pending save = %+v, %v", receipt, err)
	}
	return ws, editor, request, indexBytes
}

func recoveryWorkspacePaths(t *testing.T) (string, string) {
	t.Helper()
	return filepath.Join(t.TempDir(), "work"), filepath.Join(t.TempDir(), "registry")
}

func recoverySnapshot(t *testing.T, root, registry, id string) map[string][32]byte {
	t.Helper()
	paths := []string{filepath.Join(root, ".stow", "manifest.json"), filepath.Join(root, ".stow", "index.json"),
		filepath.Join(root, ".stow", "file-saves", "identity.json"), filepath.Join(root, ".stow", "file-saves", "pending.json"),
		filepath.Join(registry, id+".json"), filepath.Join(root, "docs", "note.txt"), filepath.Join(root, "docs", "host-only.txt")}
	result := make(map[string][32]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			result[path] = sha256.Sum256(data)
		}
	}
	return result
}

func assertRecoverySnapshot(t *testing.T, root, registry, id string, expected map[string][32]byte) {
	t.Helper()
	actual := recoverySnapshot(t, root, registry, id)
	if len(actual) != len(expected) {
		t.Fatalf("recovery file set changed: got=%d want=%d", len(actual), len(expected))
	}
	for path, digest := range expected {
		if actual[path] != digest {
			t.Fatalf("recovery modified %s", path)
		}
	}
}

func assertPayloadIdentity(t *testing.T, root string, request workspaceFileSaveRequest, before os.FileInfo) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(request.Path))
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, request.Data) {
		t.Fatalf("recovered payload=%q error=%v", data, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("recovery rewrote payload: %v", err)
	}
}
