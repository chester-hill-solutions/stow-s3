package stow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestWorkspaceEditorPublicSaveReplayAndReadOnlyRecovery(t *testing.T) {
	owner, registry, id, editor, request, first := createPublicSavedWorkspace(t)
	assertPublicSaveReplay(t, editor, request, first)
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := editor.SaveFile(context.Background(), request); !errors.Is(err, stow.ErrWorkspaceEditorClosed) || result.Outcome != stow.SaveNotCommitted {
		t.Fatalf("closed editor save = %+v, %v", result, err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	assertPublicReadOnlyRecovery(t, registry, id, request, first)
}

func createPublicSavedWorkspace(t *testing.T) (*stow.Workspace, string, string, *stow.WorkspaceEditor, stow.WorkspaceFileSaveRequest, stow.WorkspaceFileSaveResult) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	registry := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PutObject(context.Background(), owner.Bucket(), "docs/note.txt", []byte("old"), stow.PutOptions{Metadata: map[string]string{"label": "prior"}}); err != nil {
		t.Fatal(err)
	}
	editor := openPublicWorkspaceEditor(t, owner)
	observed, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	request := stow.WorkspaceFileSaveRequest{Path: observed.Path, Data: []byte("saved"), RequestKey: "public-save-1", ObservationToken: observed.ObservationToken}
	first, err := editor.SaveFile(context.Background(), request)
	if err != nil || first.Outcome != stow.SaveCommitted || first.Generation == "" || first.Object.Key != request.Path || first.Object.Data != nil {
		t.Fatalf("public save = %+v, %v", first, err)
	}
	return owner, registry, owner.ID(), editor, request, first
}

func assertPublicSaveReplay(t *testing.T, editor *stow.WorkspaceEditor, request stow.WorkspaceFileSaveRequest, first stow.WorkspaceFileSaveResult) {
	t.Helper()
	first.Object.Metadata["label"] = "changed"
	replayed, err := editor.SaveFile(context.Background(), request)
	if err != nil || !replayed.Replayed || replayed.Generation != first.Generation || replayed.Object.Metadata["label"] != "prior" {
		t.Fatalf("public replay = %+v, %v", replayed, err)
	}
	missing, err := editor.ResolveSave(context.Background(), request.Path, "missing-receipt")
	if !errors.Is(err, stow.ErrSaveRequestNotFound) || missing.Outcome != stow.SaveUnknown {
		t.Fatalf("missing receipt result = %+v, %v", missing, err)
	}
	changed := request
	changed.Data = []byte("different")
	if _, err := editor.SaveFile(context.Background(), changed); !errors.Is(err, stow.ErrSaveRequestConflict) {
		t.Fatalf("changed replay meaning error = %v", err)
	}
	invalid, err := editor.SaveFile(context.Background(), stow.WorkspaceFileSaveRequest{Path: request.Path, Data: []byte("x"), RequestKey: ""})
	if !errors.Is(err, stow.ErrInvalidSaveRequest) || invalid.Outcome != stow.SaveNotCommitted {
		t.Fatalf("invalid request result = %+v, %v", invalid, err)
	}
	if _, err := editor.SaveFile(context.Background(), stow.WorkspaceFileSaveRequest{Path: "outside.txt", Data: []byte("x"), RequestKey: "scope", Replace: true}); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("out-of-scope request error = %v", err)
	}
	if first.Object.Metadata["label"] != "changed" {
		t.Fatal("result metadata was not caller-owned")
	}
}

func assertPublicReadOnlyRecovery(t *testing.T, registry, id string, request stow.WorkspaceFileSaveRequest, first stow.WorkspaceFileSaveResult) {
	t.Helper()
	readOnly := stow.ReadOnly()
	recovery, err := stow.ResumeWorkspaceForFileRecovery(stow.WorkspaceOptions{RegistryDir: registry, Authority: &readOnly}, id)
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	recoveryEditor := openPublicWorkspaceEditor(t, recovery)
	defer recoveryEditor.Close()
	resolved, err := recoveryEditor.ResolveSave(context.Background(), request.Path, request.RequestKey)
	if err != nil || !resolved.Replayed || resolved.Outcome != stow.SaveCommitted || resolved.Generation != first.Generation || resolved.Object.Data != nil {
		t.Fatalf("read-only recovery resolution = %+v, %v", resolved, err)
	}
	_, err = recoveryEditor.SaveFile(context.Background(), stow.WorkspaceFileSaveRequest{Path: request.Path, Data: []byte("denied"), RequestKey: "readonly-write", Replace: true})
	var denied *stow.ErrNotAuthorized
	if !errors.As(err, &denied) {
		t.Fatalf("read-only save error = %v", err)
	}
}

func openPublicWorkspaceEditor(t *testing.T, owner *stow.Workspace) *stow.WorkspaceEditor {
	t.Helper()
	editor, err := owner.OpenFileEditor(stow.WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"."}, WriteRoots: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	return editor
}

func TestWorkspaceEditorPublicSaveMapsQuotaRefusal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	registry := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry, MaxBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	editor, err := owner.OpenFileEditor(stow.WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"."}, WriteRoots: []string{"."}})
	if err != nil {
		t.Fatal(err)
	}
	defer editor.Close()
	observed, err := editor.ReadForSave(context.Background(), "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	result, err := editor.SaveFile(context.Background(), stow.WorkspaceFileSaveRequest{Path: observed.Path,
		Data: []byte("four"), RequestKey: "quota", ObservationToken: observed.ObservationToken})
	if !errors.Is(err, stow.ErrQuotaExceeded) || result.Outcome != stow.SaveNotCommitted {
		t.Fatalf("quota result = %+v, %v", result, err)
	}
}
