package stow_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func openWorkspaceEditorFixture(t *testing.T, options stow.WorkspaceEditOptions) (*stow.Workspace, *stow.WorkspaceEditor, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	registryDir := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	if options.ReadRoots == nil {
		options.ReadRoots = []string{"."}
	}
	options.WriteRoots = []string{"docs"}
	options.Coordination = "host_quiesced"
	editor, err := ws.OpenFileEditor(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = editor.Close() })
	return ws, editor, registryDir
}

func TestWorkspaceEditorReadsAndMintsBoundedObservationWithoutDiskWrites(t *testing.T) {
	ws, editor, registryDir := openWorkspaceEditorFixture(t, stow.WorkspaceEditOptions{})
	manifestBefore, indexBefore := workspaceEditorMetadata(t, ws)
	registryBefore, err := os.ReadFile(filepath.Join(registryDir, ws.ID()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	file := readWorkspaceEditorTestFile(t, editor, "docs/note.txt", "initial", false)
	file.Data[0] = 'X'
	writeWorkspaceEditorTestFile(t, ws, "docs/note.txt", "changed")
	assertWorkspaceEditorRelease(t, editor, file.ObservationToken, nil)
	readWorkspaceEditorTestFile(t, editor, "docs/missing.txt", "", true)
	assertWorkspaceEditorUnavailable(t, editor, "docs/missing-parent/leaf.txt")
	assertWorkspaceEditorMetadataUnchanged(t, ws, manifestBefore, indexBefore)
	registryAfter, err := os.ReadFile(filepath.Join(registryDir, ws.ID()+".json"))
	if err != nil || !bytes.Equal(registryBefore, registryAfter) {
		t.Fatalf("observation changed registry record: %v", err)
	}
	assertWorkspaceEditorRelease(t, editor, file.ObservationToken, stow.ErrWorkspaceObservationInvalid)
}

func workspaceEditorMetadata(t *testing.T, ws *stow.Workspace) ([]byte, []byte) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(ws.Dir(), ".stow", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(ws.Dir(), ".stow", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	return manifest, index
}

func readWorkspaceEditorTestFile(t *testing.T, editor *stow.WorkspaceEditor, path, want string, absent bool) stow.WorkspaceFileRead {
	t.Helper()
	file, err := editor.ReadForSave(context.Background(), path)
	if err != nil || string(file.Data) != want || file.Absent != absent || file.ObservationToken == "" || file.ExpiresAt.IsZero() || (!absent && file.SHA256 == "") {
		t.Fatalf("file observation = %+v, %v", file, err)
	}
	return file
}

func writeWorkspaceEditorTestFile(t *testing.T, ws *stow.Workspace, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws.Dir(), filepath.FromSlash(path)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertWorkspaceEditorUnavailable(t *testing.T, editor *stow.WorkspaceEditor, path string) {
	t.Helper()
	if _, err := editor.ReadForSave(context.Background(), path); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("inaccessible path error = %v", err)
	}
}

func assertWorkspaceEditorRelease(t *testing.T, editor *stow.WorkspaceEditor, token string, want error) {
	t.Helper()
	err := editor.ReleaseObservation(token)
	if want == nil && err != nil {
		t.Fatalf("release observation: %v", err)
	}
	if want != nil && !errors.Is(err, want) {
		t.Fatalf("release error = %v, want %v", err, want)
	}
}

func assertWorkspaceEditorMetadataUnchanged(t *testing.T, ws *stow.Workspace, manifestBefore, indexBefore []byte) {
	t.Helper()
	manifestAfter, indexAfter := workspaceEditorMetadata(t, ws)
	if !bytes.Equal(manifestBefore, manifestAfter) || !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("observation changed workspace manifest or object index")
	}
}

func TestWorkspaceEditorRefusesScopeMarkerSecondEditorAndConfiguredPolicy(t *testing.T) {
	ws, editor, _ := openWorkspaceEditorFixture(t, stow.WorkspaceEditOptions{ReadRoots: []string{"docs"}})
	prior, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.OpenFileEditor(stow.WorkspaceEditOptions{Coordination: "host_quiesced", WriteRoots: []string{"docs"}}); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("second editor error = %v", err)
	}
	if _, err := editor.ReadForSave(context.Background(), "outside.txt"); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("out-of-scope read error = %v", err)
	}
	marker := filepath.Join(ws.Dir(), ".stow", "file-saves")
	if err := os.Mkdir(marker, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.ReadForSave(context.Background(), "docs/note.txt"); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("journal marker read error = %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	newEditor, err := ws.OpenFileEditor(stow.WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"."}, WriteRoots: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	defer newEditor.Close()
	if err := newEditor.ReleaseObservation(prior.ObservationToken); !errors.Is(err, stow.ErrWorkspaceObservationInvalid) {
		t.Fatalf("prior incarnation token reused: %v", err)
	}
	configured, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "policy-work"), RegistryDir: filepath.Join(t.TempDir(), "registry"), Policy: func() (policy.Set, error) { return policy.Set{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer configured.Close()
	if _, err := configured.OpenFileEditor(stow.WorkspaceEditOptions{Coordination: "host_quiesced", WriteRoots: []string{"."}}); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("configured policy open error = %v", err)
	}
}

func TestWorkspaceEditorClosesWithOwnerAndRejectsRuntimeClosed(t *testing.T) {
	ws, editor, _ := openWorkspaceEditorFixture(t, stow.WorkspaceEditOptions{})
	if err := ws.Runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.ReadForSave(context.Background(), "docs/note.txt"); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("read after direct Runtime close = %v", err)
	}
}

func TestWorkspaceEditorBoundsExpiryAndReadOnlyAuthority(t *testing.T) {
	now := time.Now().UTC()
	root := filepath.Join(t.TempDir(), "work")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry"), Now: func() time.Time { return now }, Authority: func() *stow.Authority { value := stow.ReadOnly(); return &value }()})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "read.txt"), []byte("readable"), 0o600); err != nil {
		t.Fatal(err)
	}
	editor, err := ws.OpenFileEditor(stow.WorkspaceEditOptions{ReadRoots: []string{"."}, WriteRoots: []string{"docs"}, Coordination: "host_quiesced", MaxObservations: 1, ObservationLifetime: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	read, err := editor.ReadForSave(context.Background(), "docs/read.txt")
	if err != nil || read.Generation != "" {
		t.Fatalf("read-only observation = %+v, %v", read, err)
	}
	if _, err := editor.ReadForSave(context.Background(), "docs/read.txt"); !errors.Is(err, stow.ErrWorkspaceObservationFull) {
		t.Fatalf("capacity error = %v", err)
	}
	now = now.Add(2 * time.Second)
	if err := editor.ReleaseObservation(read.ObservationToken); !errors.Is(err, stow.ErrWorkspaceObservationInvalid) {
		t.Fatalf("expired release error = %v", err)
	}
	if _, err := editor.ReadForSave(context.Background(), "docs/read.txt"); err != nil {
		t.Fatalf("read after expiry = %v", err)
	}
}

func TestWorkspaceEditorRejectsInvalidOptionsAndMissingReadGrant(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	assertWorkspaceEditorInvalidOptions(t, ws, []stow.WorkspaceEditOptions{
		{Coordination: "", ReadRoots: []string{"."}, WriteRoots: []string{"."}},
		{Coordination: "host_quiesced", ReadRoots: []string{"."}},
		{Coordination: "host_quiesced", ReadRoots: []string{"."}, WriteRoots: []string{"."}, MaxFileBytes: 65 * 1024},
		{Coordination: "host_quiesced", ReadRoots: []string{"docs"}, WriteRoots: []string{"."}},
	})
	duplicates := make([]string, maxWorkspaceEditorTestRoots)
	for index := range duplicates {
		duplicates[index] = "."
	}
	editor, err := ws.OpenFileEditor(stow.WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"."}, WriteRoots: duplicates})
	if err != nil {
		t.Fatalf("duplicate write roots should deduplicate: %v", err)
	}
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	denied, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "denied"), RegistryDir: filepath.Join(t.TempDir(), "registry"), Authority: func() *stow.Authority { value := stow.AllowNone(); return &value }()})
	if err != nil {
		t.Fatal(err)
	}
	defer denied.Close()
	if _, err := denied.OpenFileEditor(stow.WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"."}, WriteRoots: []string{"."}}); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("missing read grant error = %v", err)
	}
}

const maxWorkspaceEditorTestRoots = 17

func assertWorkspaceEditorInvalidOptions(t *testing.T, ws *stow.Workspace, optionsList []stow.WorkspaceEditOptions) {
	t.Helper()
	for _, options := range optionsList {
		if _, err := ws.OpenFileEditor(options); !errors.Is(err, stow.ErrWorkspaceEditorUnavailable) {
			t.Errorf("OpenFileEditor(%+v) error = %v", options, err)
		}
	}
}

func TestWorkspaceEditorPathSafetyAndUnicode(t *testing.T) {
	ws, editor, _ := openWorkspaceEditorFixture(t, stow.WorkspaceEditOptions{})
	prepareWorkspaceEditorPaths(t, ws)
	assertWorkspaceEditorPaths(t, editor)
}

func prepareWorkspaceEditorPaths(t *testing.T, ws *stow.Workspace) {
	t.Helper()
	for _, path := range []string{"docs/UPPER.txt", "docs/Kelvin.txt", "docs/é.txt", ".env", ".git", ".aws"} {
		full := filepath.Join(ws.Dir(), filepath.FromSlash(path))
		if path == ".git" || path == ".aws" {
			if err := os.Mkdir(full, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("value"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(ws.Dir(), "docs", "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws.Dir(), "docs", "UPPER.txt"), filepath.Join(ws.Dir(), "docs", "leaf-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(ws.Dir(), "docs"), filepath.Join(ws.Dir(), "shortcut")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
}

func assertWorkspaceEditorPaths(t *testing.T, editor *stow.WorkspaceEditor) {
	t.Helper()
	for _, path := range []string{".env", ".git", ".git/config", ".aws", ".aws/credentials", "docs/upper.txt", "docs/kelvin.txt", "docs/folder", "docs/leaf-link", "shortcut/UPPER.txt"} {
		assertWorkspaceEditorUnavailable(t, editor, path)
	}
	readWorkspaceEditorTestFile(t, editor, "docs/UPPER.txt", "value", false)
	readWorkspaceEditorTestFile(t, editor, "docs/Kelvin.txt", "value", false)
	readWorkspaceEditorTestFile(t, editor, "docs/é.txt", "value", false)
}

func TestWorkspaceEditorEmptyBoundsAndCancellation(t *testing.T) {
	ws, editor, _ := openWorkspaceEditorFixture(t, stow.WorkspaceEditOptions{MaxFileBytes: 4})
	prepareWorkspaceEditorSizeFiles(t, ws)
	assertWorkspaceEditorSizes(t, editor)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := editor.ReadForSave(ctx, "docs/four.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read error = %v", err)
	}
}

func prepareWorkspaceEditorSizeFiles(t *testing.T, ws *stow.Workspace) {
	t.Helper()
	for name, body := range map[string]string{"empty.txt": "", "four.txt": "1234", "five.txt": "12345"} {
		writeWorkspaceEditorTestFile(t, ws, filepath.Join("docs", name), body)
	}
	if err := os.MkdirAll(filepath.Join(ws.Dir(), "inputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceEditorTestFile(t, ws, "inputs/source.txt", "in")
}

func assertWorkspaceEditorSizes(t *testing.T, editor *stow.WorkspaceEditor) {
	t.Helper()
	readWorkspaceEditorTestFile(t, editor, "docs/empty.txt", "", false)
	readWorkspaceEditorTestFile(t, editor, "docs/four.txt", "1234", false)
	if _, err := editor.ReadForSave(context.Background(), "docs/five.txt"); !errors.Is(err, stow.ErrWorkspaceReadTooLarge) {
		t.Fatalf("over-limit read error = %v", err)
	}
	readWorkspaceEditorTestFile(t, editor, "inputs/source.txt", "in", false)
}

func TestWorkspaceEditorRootReplacementRefusesObservation(t *testing.T) {
	ws, editor, _ := openWorkspaceEditorFixture(t, stow.WorkspaceEditOptions{})
	root := ws.Dir()
	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceEditorUnavailable(t, editor, "docs/note.txt")
}

func TestWorkspaceEditorOwnerCloseSerializesWithRead(t *testing.T) {
	ws, editor, _ := openWorkspaceEditorFixture(t, stow.WorkspaceEditOptions{})
	start := make(chan struct{})
	readDone := make(chan error, 1)
	closeDone := make(chan error, 1)
	go func() { <-start; _, err := editor.ReadForSave(context.Background(), "docs/note.txt"); readDone <- err }()
	go func() { <-start; closeDone <- ws.Close() }()
	close(start)
	readErr, closeErr := <-readDone, <-closeDone
	if closeErr != nil {
		t.Fatalf("owner close error = %v", closeErr)
	}
	if readErr != nil && !errors.Is(readErr, stow.ErrWorkspaceEditorClosed) && !errors.Is(readErr, stow.ErrWorkspaceEditorUnavailable) {
		t.Fatalf("concurrent read error = %v", readErr)
	}
	if _, err := editor.ReadForSave(context.Background(), "docs/note.txt"); !errors.Is(err, stow.ErrWorkspaceEditorClosed) {
		t.Fatalf("read after owner close = %v", err)
	}
}
