package stow_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func workspaceReaderFixture(t *testing.T) (*stow.Workspace, string) {
	t.Helper()
	registryDir := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registryDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws, registryDir
}

func TestWorkspaceReaderReadsHostFilesAlongsideLiveWorkspace(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	if err := os.MkdirAll(filepath.Join(ws.Dir(), "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "docs", "readme.txt"), []byte("written by host"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatalf("open reader beside live writer: %v", err)
	}
	defer reader.Close()
	if reader.Reference().ID != ws.ID() || reader.Reference().Bucket != ws.Bucket() {
		t.Fatalf("reader reference = %+v", reader.Reference())
	}
	assertWorkspaceReaderHostRead(t, reader)
	assertWorkspaceReaderNavigation(t, reader)
	if _, err := stow.CheckpointOf(context.Background(), registryDir, ws.ID(), stow.CheckpointOptions{}); err != nil {
		t.Fatalf("checkpoint capture should coexist with reader: %v", err)
	}
}

func assertWorkspaceReaderHostRead(t *testing.T, reader *stow.WorkspaceReader) {
	t.Helper()
	file, err := reader.ReadFile(context.Background(), "docs/readme.txt")
	if err != nil || string(file.Data) != "written by host" {
		t.Fatalf("read host file = %q, %v", file.Data, err)
	}
}

func assertWorkspaceReaderNavigation(t *testing.T, reader *stow.WorkspaceReader) {
	t.Helper()
	entries, err := reader.ReadDirectory(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "docs" || entries[0].Kind != "directory" {
		t.Fatalf("root entries = %+v", entries)
	}
	children, err := reader.ReadDirectory(context.Background(), "docs")
	if err != nil || len(children) != 1 || children[0].Name != "readme.txt" {
		t.Fatalf("docs entries = %+v, %v", children, err)
	}
}

func TestWorkspaceReaderLeaseBlocksRemovalAfterWriterCloses(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	registry, err := workspace.OpenRegistryReadOnly(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = registry.RemoveWorkspace(ws.ID(), func() error { called = true; return nil })
	if !errors.Is(err, workspace.ErrWorkspaceInUse) || called {
		t.Fatalf("removal with reader = %v, callback called=%v", err, called)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := registry.RemoveWorkspace(ws.ID(), func() error { return nil }); err != nil {
		t.Fatalf("removal after reader close: %v", err)
	}
}

func TestWorkspaceReaderConstructionDoesNotChangeIdentityOrIndex(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	manifestPath := filepath.Join(ws.Dir(), ".stow", "manifest.json")
	indexPath := filepath.Join(ws.Dir(), ".stow", "index.json")
	manifestBefore, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: "missing-id"}); err == nil {
		t.Fatal("opened an unregistered workspace")
	}
	manifestAfter, _ := os.ReadFile(manifestPath)
	indexAfter, _ := os.ReadFile(indexPath)
	if !bytes.Equal(manifestBefore, manifestAfter) || !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("reader construction changed workspace identity or index")
	}
}

func TestWorkspaceReaderRejectsManifestIDMismatchAndReleasesLease(t *testing.T) {
	assertWorkspaceReaderRejectsManifestMismatch(t, "workspace_id")
}

func TestWorkspaceReaderRejectsManifestBucketMismatchAndReleasesLease(t *testing.T) {
	assertWorkspaceReaderRejectsManifestMismatch(t, "bucket")
}

func assertWorkspaceReaderRejectsManifestMismatch(t *testing.T, field string) {
	t.Helper()
	ws, registryDir := workspaceReaderFixture(t)
	manifestPath := filepath.Join(ws.Dir(), ".stow", "manifest.json")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	value := ws.ID()
	if field == "bucket" {
		value = ws.Bucket()
	}
	needle := `"` + field + `": "` + value + `"`
	broken := bytes.Replace(before, []byte(needle), []byte(`"`+field+`": "wrong"`), 1)
	if bytes.Equal(before, broken) {
		t.Fatalf("could not alter manifest %s", field)
	}
	if err := os.WriteFile(manifestPath, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()}); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
		t.Fatalf("reader accepted manifest %s mismatch: %v", field, err)
	}
	if err := os.WriteFile(manifestPath, before, 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := workspace.OpenRegistryReadOnly(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := registry.RemoveWorkspace(ws.ID(), func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("removal after failed construction = %v, callback called=%v", err, called)
	}
}

func TestWorkspaceReaderRefusesLeafSymlinkAndOmitsItFromDirectory(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	target := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(ws.Dir(), "linked.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.ReadFile(context.Background(), "linked.txt"); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
		t.Fatalf("read symlink leaf = %v", err)
	}
	entries, err := reader.ReadDirectory(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name == "linked.txt" {
			t.Fatal("directory listed symlink leaf")
		}
	}
}

func TestWorkspaceReaderCaseFoldsSensitiveNameAliases(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	credentials := filepath.Join(ws.Dir(), "credentials")
	if err := os.Mkdir(credentials, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(credentials, "private.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertReaderPathUnavailable(t, reader, "credentials/private.txt")
	assertReaderPathUnavailable(t, reader, "credentialſ/private.txt")
	entries, err := reader.ReadDirectory(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	assertEntryAbsent(t, entries, "credentials")
	assertCaseAliasRefuses(t, reader, ws.Dir(), ".stow/manifest.json", ".ſtow/manifest.json")
	assertCaseAliasRefuses(t, reader, ws.Dir(), "credentials/private.txt", "credentialſ/private.txt")
}

func assertReaderPathUnavailable(t *testing.T, reader *stow.WorkspaceReader, path string) {
	t.Helper()
	if _, err := reader.ReadFile(context.Background(), path); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
}

func assertEntryAbsent(t *testing.T, entries []stow.WorkspaceReadEntry, name string) {
	t.Helper()
	for _, entry := range entries {
		if entry.Name == name {
			t.Fatalf("directory listed sensitive entry %q", name)
		}
	}
}

func assertCaseAliasRefuses(t *testing.T, reader *stow.WorkspaceReader, root, target, alias string) {
	t.Helper()
	targetInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(target)))
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(alias)))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(aliasInfo, targetInfo) {
		return
	}
	assertReaderPathUnavailable(t, reader, alias)
}

func TestWorkspaceReaderRejectsPrivateInvalidAndOversizeReads(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	for path, body := range map[string]string{
		".env.local": "secret", ".env/secret.txt": "nested secret", ".stow/nope": "private", ".git/config": "git", ".aws/credentials": "aws", "private/.env": "nested",
		"ok.txt": "12345",
	} {
		full := filepath.Join(ws.Dir(), filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), MaxFileBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, path := range []string{"../ok.txt", "/ok.txt", `a\\b`, ".env.local", ".env/secret.txt", ".stow/nope", ".git/config", ".aws/credentials", "private/.env"} {
		if _, err := reader.ReadFile(context.Background(), path); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
			t.Errorf("ReadFile(%q) error = %v", path, err)
		}
	}
	if _, err := reader.ReadFile(context.Background(), "ok.txt"); !errors.Is(err, stow.ErrWorkspaceReadTooLarge) {
		t.Fatalf("oversize file error = %v", err)
	}
	entries, err := reader.ReadDirectory(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	privateListed := false
	for _, entry := range entries {
		if entry.Name == "private" {
			privateListed = true
		}
		if strings.HasPrefix(entry.Name, ".") {
			t.Fatalf("sensitive child listed: %+v", entry)
		}
	}
	if !privateListed {
		t.Fatal("ordinary private directory was omitted")
	}
	children, err := reader.ReadDirectory(context.Background(), "private")
	if err != nil || len(children) != 0 {
		t.Fatalf("sensitive nested child was not excluded: %+v, %v", children, err)
	}
}

func TestWorkspaceReaderEnforcesEntryBoundsAndClose(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	for i := range 3 {
		if err := os.WriteFile(filepath.Join(ws.Dir(), "f"+string(rune('0'+i))), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadDirectory(context.Background(), "."); !errors.Is(err, stow.ErrWorkspaceReadTooLarge) {
		t.Fatalf("directory overflow error = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := reader.ReadDirectory(context.Background(), "."); !errors.Is(err, stow.ErrWorkspaceReaderClosed) {
		t.Fatalf("read after close error = %v", err)
	}
}

func TestWorkspaceReaderRefusesRootAndIntermediateSymlinkReplacement(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	docs := filepath.Join(ws.Dir(), "docs")
	if err := os.Mkdir(docs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "readme.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "readme.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(docs, docs+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, docs); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := reader.ReadFile(context.Background(), "docs/readme.txt"); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
		t.Fatalf("read through replaced intermediate symlink = %v", err)
	}
	if _, err := reader.ReadDirectory(context.Background(), "docs"); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
		t.Fatalf("list through replaced intermediate symlink = %v", err)
	}
	root := ws.Dir()
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root+"-old", root); err != nil {
		t.Skipf("root symlink unavailable: %v", err)
	}
	if _, err := reader.ReadDirectory(context.Background(), "."); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) && !errors.Is(err, stow.ErrWorkspaceReadChanged) {
		t.Fatalf("read after root replacement = %v", err)
	}
}
