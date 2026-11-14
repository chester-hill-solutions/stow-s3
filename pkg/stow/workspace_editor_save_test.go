package stow

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceEditorSaveReplaySurvivesReleaseExpiryAndIncarnation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	makeEditorWorkspace(t, root)
	now := time.Now().UTC()
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	editor := openPrivateEditor(t, ws, []string{"."}, []string{"docs"})
	defer editor.Close()
	request, first := createEditorSave(t, editor)
	assertReleasedReplay(t, editor, request, first, &now)
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	editor = openPrivateEditor(t, ws, []string{"."}, []string{"docs"})
	defer editor.Close()
	assertMeaningConflict(t, editor, request)
	assertReceiptReplay(t, editor, request, first, "new-incarnation replay")
	assertLaterPayloadPreserved(t, ws, editor, request, first)
}

func createEditorSave(t *testing.T, editor *WorkspaceEditor) (workspaceFileSaveRequest, storage.SaveReceipt) {
	t.Helper()
	read, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	request := workspaceFileSaveRequest{Path: read.Path, Data: []byte("saved"), RequestKey: "save-1", ObservationToken: read.ObservationToken}
	receipt, err := editor.saveFile(context.Background(), request)
	if err != nil || receipt.Meta == nil || receipt.Outcome != string(storage.WorkspaceFileCommitted) || receipt.Replayed {
		t.Fatalf("initial save = %+v, %v", receipt, err)
	}
	return request, receipt
}

func assertReleasedReplay(t *testing.T, editor *WorkspaceEditor, request workspaceFileSaveRequest, first storage.SaveReceipt, now *time.Time) {
	t.Helper()
	if err := editor.ReleaseObservation(request.ObservationToken); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(10 * time.Minute)
	assertReceiptReplay(t, editor, request, first, "released/expired replay")
}

func assertMeaningConflict(t *testing.T, editor *WorkspaceEditor, request workspaceFileSaveRequest) {
	t.Helper()
	changed := []workspaceFileSaveRequest{request, request, request, request}
	changed[0].Data = []byte("different")
	changed[1].ObservationToken = "different-token"
	changed[2].Replace = true
	changed[2].ObservationToken = ""
	changed[3].Path = "docs/other.txt"
	for _, candidate := range changed {
		if _, err := editor.saveFile(context.Background(), candidate); !errors.Is(err, storage.ErrSaveRequestConflict) {
			t.Fatalf("changed replay meaning error = %v", err)
		}
	}
}

func assertReceiptReplay(t *testing.T, editor *WorkspaceEditor, request workspaceFileSaveRequest, first storage.SaveReceipt, label string) {
	t.Helper()
	replayed, err := editor.saveFile(context.Background(), request)
	if err != nil || !replayed.Replayed || !sameReceiptMeta(first.Meta, replayed.Meta) {
		t.Fatalf("%s = %+v, %v", label, replayed, err)
	}
}

func assertLaterPayloadPreserved(t *testing.T, ws *Workspace, editor *WorkspaceEditor, request workspaceFileSaveRequest, first storage.SaveReceipt) {
	t.Helper()
	if _, err := ws.PutObject(context.Background(), ws.Bucket(), "docs/note.txt", []byte("newer"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	assertReceiptReplay(t, editor, request, first, "replay after later write")
	data, err := os.ReadFile(filepath.Join(ws.Dir(), "docs", "note.txt"))
	if err != nil || !bytes.Equal(data, []byte("newer")) {
		t.Fatalf("later payload was rewritten: %q, %v", data, err)
	}
}

func TestWorkspaceEditorSaveScopeAuthorityAndResolveReadRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	makeEditorWorkspace(t, root)
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	first := openPrivateEditor(t, ws, []string{"."}, []string{"docs"})
	read, err := first.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	req := workspaceFileSaveRequest{Path: read.Path, Data: []byte("saved"), RequestKey: "scope-save", ObservationToken: read.ObservationToken}
	if _, err := first.saveFile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := first.saveFile(context.Background(), workspaceFileSaveRequest{Path: "outside.txt", Data: []byte("x"), RequestKey: "outside", Replace: true}); !errors.Is(err, ErrWorkspaceEditorUnavailable) {
		t.Fatalf("outside write scope error = %v", err)
	}
	if _, err := first.saveFile(context.Background(), workspaceFileSaveRequest{Path: "docs/note.txt", Data: []byte("x"), RequestKey: "invalid", Replace: true, ObservationToken: "unexpected"}); !errors.Is(err, storage.ErrInvalidSaveRequest) {
		t.Fatalf("invalid replace/token syntax error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "docs", "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	second := openPrivateEditor(t, ws, []string{"docs"}, []string{"docs/subdir"})
	defer second.Close()
	resolved, err := second.resolveSave(context.Background(), "docs/note.txt", req.RequestKey)
	if err != nil || !resolved.Replayed || resolved.Meta == nil {
		t.Fatalf("read-scope resolution = %+v, %v", resolved, err)
	}
	if _, err := second.resolveSave(context.Background(), "outside.txt", req.RequestKey); !errors.Is(err, ErrWorkspaceEditorUnavailable) {
		t.Fatalf("out-of-read-scope resolution error = %v", err)
	}
	if _, err := second.saveFile(context.Background(), workspaceFileSaveRequest{Path: "docs/note.txt", Data: []byte("fresh"), RequestKey: "new-write", Replace: true}); !errors.Is(err, ErrWorkspaceEditorUnavailable) {
		t.Fatalf("narrow write scope save error = %v", err)
	}
}

func TestWorkspaceEditorResolvesPublishedIndexFailureWithoutRepublishing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	makeEditorWorkspace(t, root)
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	editor := openPrivateEditor(t, ws, []string{"."}, []string{"docs"})
	request, indexPath := forceEditorIndexSaveFailure(t, ws, editor)
	assertEditorSavePaused(t, editor, request)
	publishedInfo, err := os.Stat(filepath.Join(root, "docs", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	editor = openPrivateEditor(t, ws, []string{"."}, []string{"docs"})
	defer editor.Close()
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	assertEditorResolveResumes(t, editor, request, publishedInfo)
}

func forceEditorIndexSaveFailure(t *testing.T, ws *Workspace, editor *WorkspaceEditor) (workspaceFileSaveRequest, string) {
	t.Helper()
	read, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(ws.Dir(), ".stow", "index.json")
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexPath, 0o700); err != nil {
		t.Fatal(err)
	}
	return workspaceFileSaveRequest{Path: read.Path, Data: []byte("committed once"), RequestKey: "index-error", ObservationToken: read.ObservationToken}, indexPath
}

func assertEditorSavePaused(t *testing.T, editor *WorkspaceEditor, request workspaceFileSaveRequest) {
	t.Helper()
	receipt, err := editor.saveFile(context.Background(), request)
	if !errors.Is(err, storage.ErrSaveRequestUnknown) || receipt.Outcome != string(storage.WorkspaceFileUnknown) {
		t.Fatalf("save with blocked index = %+v, %v", receipt, err)
	}
	if _, err := editor.ReadForSave(context.Background(), request.Path); !errors.Is(err, ErrWorkspaceEditorUnavailable) {
		t.Fatalf("observation while paused error = %v", err)
	}
}

func assertEditorResolveResumes(t *testing.T, editor *WorkspaceEditor, request workspaceFileSaveRequest, publishedInfo os.FileInfo) {
	t.Helper()
	resolved, err := editor.resolveSave(context.Background(), request.Path, request.RequestKey)
	if err != nil || resolved.Outcome != string(storage.WorkspaceFileCommitted) || resolved.Meta == nil {
		t.Fatalf("resolve after index repair = %+v, %v", resolved, err)
	}
	data, err := os.ReadFile(filepath.Join(editor.owner.Dir(), "docs", "note.txt"))
	if err != nil || !bytes.Equal(data, request.Data) {
		t.Fatalf("resolved payload = %q, %v", data, err)
	}
	currentInfo, err := os.Stat(filepath.Join(editor.owner.Dir(), "docs", "note.txt"))
	if err != nil || !os.SameFile(publishedInfo, currentInfo) || !publishedInfo.ModTime().Equal(currentInfo.ModTime()) {
		t.Fatalf("resolve rewrote payload: prior=%v current=%v error=%v", publishedInfo, currentInfo, err)
	}
	if _, err := editor.ReadForSave(context.Background(), "docs/note.txt"); err != nil {
		t.Fatalf("observation after resolve = %v", err)
	}
	fresh := workspaceFileSaveRequest{Path: request.Path, Data: []byte("next"), RequestKey: "after-resolve", Replace: true}
	if next, err := editor.saveFile(context.Background(), fresh); err != nil || next.Outcome != string(storage.WorkspaceFileCommitted) {
		t.Fatalf("fresh save after resolve = %+v, %v", next, err)
	}
}

func TestWorkspaceEditorSaveCancellationAndReadOnlyRefusal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	makeEditorWorkspace(t, root)
	readOnly := ReadOnly()
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry"), Authority: &readOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	editor := openPrivateEditor(t, ws, []string{"."}, []string{"docs"})
	defer editor.Close()
	read, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	request := workspaceFileSaveRequest{Path: read.Path, Data: []byte("denied"), RequestKey: "readonly", ObservationToken: read.ObservationToken}
	if _, err := editor.saveFile(canceled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save error = %v", err)
	}
	if _, err := editor.saveFile(context.Background(), request); err == nil {
		t.Fatal("read-only authority permitted Save")
	}
	if _, err := os.Stat(filepath.Join(root, ".stow", "file-saves")); !os.IsNotExist(err) {
		t.Fatalf("refused save created durable journal: %v", err)
	}
}

func makeEditorWorkspace(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func openPrivateEditor(t *testing.T, ws *Workspace, readRoots, writeRoots []string) *WorkspaceEditor {
	t.Helper()
	editor, err := ws.OpenFileEditor(WorkspaceEditOptions{Coordination: workspaceFileCoordinationHostQuiesced, ReadRoots: readRoots, WriteRoots: writeRoots})
	if err != nil {
		t.Fatal(err)
	}
	return editor
}

func sameReceiptMeta(left, right *storage.ObjectMeta) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Bucket == right.Bucket && left.Key == right.Key && left.VersionID == right.VersionID && left.ETag == right.ETag && left.Size == right.Size &&
		left.LastModified.Equal(right.LastModified) && left.ContentType == right.ContentType && left.ChecksumAlgorithm == right.ChecksumAlgorithm &&
		left.ChecksumValue == right.ChecksumValue && equalStringMetadata(left.Metadata, right.Metadata)
}

func equalStringMetadata(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
