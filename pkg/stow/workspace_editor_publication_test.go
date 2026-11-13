package stow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceEditorPublishesAgainstObservationAndRejectsStaleToken(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	editor, err := ws.OpenFileEditor(WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"docs"}, WriteRoots: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	defer editor.Close()
	observation, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	publication, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/note.txt", Data: []byte("saved"), ObservationToken: observation.ObservationToken})
	if err != nil || publication.Outcome != "committed" || !publication.IndexReady || publication.Meta == nil {
		t.Fatalf("publish=%+v error=%v", publication, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "note.txt"))
	if err != nil || string(data) != "saved" {
		t.Fatalf("saved bytes=%q error=%v", data, err)
	}
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/note.txt", Data: []byte("stale"), ObservationToken: observation.ObservationToken}); !errors.Is(err, storage.ErrWorkspaceFileConflict) {
		t.Fatalf("reused observation error=%v", err)
	}
}

func TestWorkspaceEditorCanCreateOnlyWithinWriteRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	editor, err := ws.OpenFileEditor(WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"."}, WriteRoots: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	defer editor.Close()
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "outside.txt", Data: []byte("no"), Replace: true}); !errors.Is(err, ErrWorkspaceEditorUnavailable) {
		t.Fatalf("out-of-write-scope publication error=%v", err)
	}
	publication, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/new.txt", Data: []byte("created"), Replace: true})
	if err != nil || publication.Outcome != "committed" {
		t.Fatalf("create=%+v error=%v", publication, err)
	}
}

func TestWorkspaceEditorRejectsForeignAndWrongPathTokens(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "UPPER.txt"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, editor := openPublicationEditor(t, root, WorkspaceEditOptions{ReadRoots: []string{"."}})
	defer ws.Close()
	defer editor.Close()
	observation, err := editor.ReadForSave(context.Background(), "docs/UPPER.txt")
	if err != nil {
		t.Fatal(err)
	}
	foreignRoot := filepath.Join(t.TempDir(), "foreign")
	if err := os.MkdirAll(filepath.Join(foreignRoot, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignRoot, "docs", "UPPER.txt"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreignWS, foreignEditor := openPublicationEditor(t, foreignRoot, WorkspaceEditOptions{ReadRoots: []string{"docs"}})
	foreignObservation, err := foreignEditor.ReadForSave(context.Background(), "docs/UPPER.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/UPPER.txt", Data: []byte("foreign"), ObservationToken: foreignObservation.ObservationToken}); !errors.Is(err, ErrWorkspaceObservationInvalid) {
		t.Fatalf("foreign-owner token error=%v", err)
	}
	_ = foreignEditor.Close()
	_ = foreignWS.Close()
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/other.txt", Data: []byte("x"), ObservationToken: observation.ObservationToken}); !errors.Is(err, ErrWorkspaceObservationInvalid) {
		t.Fatalf("wrong-path token error=%v", err)
	}
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/UPPER.txt", Data: []byte("x"), ObservationToken: "not-a-token"}); !errors.Is(err, ErrWorkspaceObservationInvalid) {
		t.Fatalf("invalid token error=%v", err)
	}
}

func TestWorkspaceEditorCancellationAndUnsafeTargets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "UPPER.txt"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, editor := openPublicationEditor(t, root, WorkspaceEditOptions{ReadRoots: []string{"."}})
	defer ws.Close()
	defer editor.Close()
	observation, err := editor.ReadForSave(context.Background(), "docs/UPPER.txt")
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := editor.publishFile(canceled, workspaceFilePublishRequest{Path: "docs/UPPER.txt", Data: []byte("cancelled"), ObservationToken: observation.ObservationToken}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save error=%v", err)
	}
	for _, request := range []workspaceFilePublishRequest{
		{Path: "docs/upper.txt", Data: []byte("alias"), Replace: true},
		{Path: "docs/missing-parent/leaf.txt", Data: []byte("no dirs"), Replace: true},
	} {
		if _, err := editor.publishFile(context.Background(), request); err == nil {
			t.Fatalf("unsafe target unexpectedly published: %+v", request)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "missing-parent")); !os.IsNotExist(err) {
		t.Fatalf("save created missing parent: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "UPPER.txt"))
	if err != nil || string(data) != "stable" {
		t.Fatalf("refused saves changed source bytes=%q error=%v", data, err)
	}
}

func TestWorkspaceEditorRequiresWriteAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	readOnly := ReadOnly()
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry"), Authority: &readOnly})
	if err != nil {
		t.Fatal(err)
	}
	editor, err := ws.OpenFileEditor(WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"docs"}, WriteRoots: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	readonlyBasis, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/note.txt", Data: []byte("denied"), ObservationToken: readonlyBasis.ObservationToken}); err == nil {
		t.Fatal("read-only authority allowed file publication")
	}
	_ = editor.Close()
	_ = ws.Close()
}

func TestWorkspaceEditorDetectsMetadataABAAndManagedChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "writable")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, editor := openPublicationEditor(t, root, WorkspaceEditOptions{ReadRoots: []string{"docs"}})
	defer ws.Close()
	defer editor.Close()
	modeBasis, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "docs", "note.txt"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/note.txt", Data: []byte("mode"), ObservationToken: modeBasis.ObservationToken}); !errors.Is(err, storage.ErrWorkspaceFileConflict) {
		t.Fatalf("chmod basis error=%v", err)
	}
	abaBasis, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "docs", "replacement.txt")
	if err := os.WriteFile(replacement, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filepath.Join(root, "docs", "note.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/note.txt", Data: []byte("ABA"), ObservationToken: abaBasis.ObservationToken}); !errors.Is(err, storage.ErrWorkspaceFileConflict) {
		t.Fatalf("same-bytes replacement basis error=%v", err)
	}
	managedBasis, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.PutObject(context.Background(), ws.Bucket(), "docs/note.txt", []byte("managed"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/note.txt", Data: []byte("stale"), ObservationToken: managedBasis.ObservationToken}); !errors.Is(err, storage.ErrWorkspaceFileConflict) {
		t.Fatalf("managed basis error=%v", err)
	}
}

func TestWorkspaceEditorRejectsExpiredSaveObservation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	now := time.Now().UTC()
	ws.now = func() time.Time { return now }
	editor, err := ws.OpenFileEditor(WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"docs"}, WriteRoots: []string{"docs"}, ObservationLifetime: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer editor.Close()
	observation, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err := editor.publishFile(context.Background(), workspaceFilePublishRequest{Path: "docs/note.txt", Data: []byte("expired"), ObservationToken: observation.ObservationToken}); !errors.Is(err, ErrWorkspaceObservationInvalid) {
		t.Fatalf("expired observation error=%v", err)
	}
}

func openPublicationEditor(t *testing.T, root string, options WorkspaceEditOptions) (*Workspace, *WorkspaceEditor) {
	t.Helper()
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")})
	if err != nil {
		t.Fatal(err)
	}
	options.Coordination = "host_quiesced"
	options.WriteRoots = []string{"docs"}
	editor, err := ws.OpenFileEditor(options)
	if err != nil {
		_ = ws.Close()
		t.Fatal(err)
	}
	return ws, editor
}
