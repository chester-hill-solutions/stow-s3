package stow_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestCheckpointExcludesSensitiveAndGitFiles(t *testing.T) {
	root, registry, ws := openCheckpointTestWorkspace(t)
	writeTaskFile(t, root, "main.go", "package task\n")
	writeTaskFile(t, root, ".env", "TOKEN=excluded\n")
	writeTaskFile(t, root, ".git", "gitdir: external\n")
	writeTaskFile(t, root, "repo/.git/config", "[remote]\nurl=https://example.invalid\n")
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if len(checkpoint.Excluded) != 1 || checkpoint.Excluded[0] != ".env" {
		t.Fatalf("excluded paths = %v, want .env", checkpoint.Excluded)
	}
	if _, err := os.Stat(filepath.Join(registry, "checkpoints", checkpoint.ID, "files", ".git")); !os.IsNotExist(err) {
		t.Fatalf("Git metadata file was included in checkpoint: %v", err)
	}
	if _, err := os.Stat(filepath.Join(registry, "checkpoints", checkpoint.ID, "files", "repo", ".git")); !os.IsNotExist(err) {
		t.Fatalf("nested Git metadata was included in checkpoint: %v", err)
	}
}

func TestCheckpointDiffCapturesAddedChangedAndDeletedFiles(t *testing.T) {
	root, registry, ws := openCheckpointTestWorkspace(t)
	writeTaskFile(t, root, "src/keep.go", "package task\n")
	writeTaskFile(t, root, "removed.txt", "old\n")
	first, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}
	writeTaskFile(t, root, "src/keep.go", "package task\n// changed\n")
	writeTaskFile(t, root, "new.txt", "new\n")
	if err := os.Remove(filepath.Join(root, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	second, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{ParentID: first.ID})
	if err != nil {
		t.Fatalf("second checkpoint: %v", err)
	}
	changes, err := stow.CompareCheckpoints(registry, first.ID, second.ID)
	if err != nil {
		t.Fatalf("CompareCheckpoints: %v", err)
	}
	assertCheckpointChanges(t, changes)
}

func TestRestoreCreatesANewWorkspaceFromImmutableCheckpoint(t *testing.T) {
	root, registry, ws := openCheckpointTestWorkspace(t)
	writeTaskFile(t, root, "src/keep.go", "package task\n")
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{MaxBytes: 1000, MaxFiles: 10})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	restored, err := stow.RestoreCheckpoint(registry, checkpoint.ID, stow.WorkspaceOptions{
		Dir: filepath.Join(t.TempDir(), "restored"), RegistryDir: registry,
	})
	if err != nil {
		t.Fatalf("RestoreCheckpoint: %v", err)
	}
	defer restored.Close()
	if restored.ID() == ws.ID() || restored.ID() == checkpoint.ID {
		t.Fatal("restore reused the mutable workspace or checkpoint ID")
	}
	if got, err := os.ReadFile(filepath.Join(restored.Dir(), "src", "keep.go")); err != nil || string(got) != "package task\n" {
		t.Fatalf("restored file = %q, %v", got, err)
	}
}

func openCheckpointTestWorkspace(t *testing.T) (string, string, *stow.Workspace) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	registry := registryDir(t)
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return root, registry, ws
}

func assertCheckpointChanges(t *testing.T, changes []stow.CheckpointChange) {
	t.Helper()
	want := []stow.CheckpointChange{{Path: "new.txt", Kind: "added"}, {Path: "removed.txt", Kind: "deleted"}, {Path: "src/keep.go", Kind: "changed"}}
	if len(changes) != len(want) {
		t.Fatalf("checkpoint diff = %+v, want %d changes", changes, len(want))
	}
	for i := range want {
		if changes[i].Path != want[i].Path || changes[i].Kind != want[i].Kind {
			t.Fatalf("checkpoint diff = %+v, want deterministic path-sorted changes", changes)
		}
	}
}

func TestCheckpointRefusesCorruptFilesAndEnforcesLimits(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	registry := registryDir(t)
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer ws.Close()
	writeTaskFile(t, root, "large.txt", "larger than three bytes")
	if _, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{MaxBytes: 3}); err == nil {
		t.Fatal("checkpoint ignored its byte cap")
	}
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(registry, "checkpoints", checkpoint.ID, "files", "large.txt"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = stow.RestoreCheckpoint(registry, checkpoint.ID, stow.WorkspaceOptions{
		Dir: filepath.Join(t.TempDir(), "restored"), RegistryDir: registry,
	})
	if err == nil {
		t.Fatal("RestoreCheckpoint accepted corrupt checkpoint bytes")
	}
}

func TestCheckpointArchiveRoundTripsToAnotherRegistryAndRestores(t *testing.T) {
	root, sourceRegistry, ws := openCheckpointTestWorkspace(t)
	writeTaskFile(t, root, "src/main.go", "package task\n")
	writeTaskFile(t, root, ".env", "TOKEN=private\n")
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	var archive bytes.Buffer
	if err := stow.ExportCheckpoint(context.Background(), sourceRegistry, checkpoint.ID, &archive, stow.CheckpointArchiveOptions{}); err != nil {
		t.Fatalf("ExportCheckpoint: %v", err)
	}
	destinationRegistry := registryDir(t)
	imported, err := stow.ImportCheckpoint(context.Background(), destinationRegistry, bytes.NewReader(archive.Bytes()), stow.CheckpointArchiveOptions{})
	if err != nil {
		t.Fatalf("ImportCheckpoint: %v", err)
	}
	if imported.ID != checkpoint.ID || imported.Files != 1 || len(imported.Excluded) != 1 || imported.Excluded[0] != ".env" {
		t.Fatalf("imported checkpoint = %+v", imported)
	}
	restored, err := stow.RestoreCheckpoint(destinationRegistry, imported.ID, stow.WorkspaceOptions{
		Dir: filepath.Join(t.TempDir(), "restored"), RegistryDir: destinationRegistry,
	})
	if err != nil {
		t.Fatalf("RestoreCheckpoint imported archive: %v", err)
	}
	defer restored.Close()
	if data, err := os.ReadFile(filepath.Join(restored.Dir(), "src", "main.go")); err != nil || string(data) != "package task\n" {
		t.Fatalf("restored imported file = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(restored.Dir(), ".env")); !os.IsNotExist(err) {
		t.Fatalf("excluded secret appeared in restored workspace: %v", err)
	}
}

func TestCheckpointArchiveRequiresSensitivePathOptIn(t *testing.T) {
	root, registry, ws := openCheckpointTestWorkspace(t)
	writeTaskFile(t, root, ".env", "TOKEN=private\n")
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{IncludeSensitiveFiles: true})
	if err != nil {
		t.Fatalf("CreateCheckpoint with explicit sensitive input: %v", err)
	}
	if err := stow.ExportCheckpoint(context.Background(), registry, checkpoint.ID, &bytes.Buffer{}, stow.CheckpointArchiveOptions{}); err == nil {
		t.Fatal("export included a sensitive-looking path without an export opt-in")
	}
	if err := stow.ExportCheckpoint(context.Background(), registry, checkpoint.ID, &bytes.Buffer{}, stow.CheckpointArchiveOptions{IncludeSensitiveFiles: true}); err != nil {
		t.Fatalf("explicit sensitive export: %v", err)
	}
}

func TestCheckpointImportRejectsCorruptAndTraversingArchives(t *testing.T) {
	registry := registryDir(t)
	content := []byte("right")
	digest := sha256.Sum256(content)
	manifest := stow.CheckpointManifest{
		Version: 1, ID: "cp_aaaaaaaaaaaaaaaaaaaaaaaa", WorkspaceID: "ws_archive_test",
		Created: time.Unix(1, 0).UTC(),
		Files:   []stow.CheckpointFile{{Path: "safe.txt", Size: int64(len(content)), Mode: 0o600, SHA256: hex.EncodeToString(digest[:])}},
	}
	corrupt := makeCheckpointArchive(t, manifest, []byte("wrong"))
	if _, err := stow.ImportCheckpoint(context.Background(), registry, bytes.NewReader(corrupt), stow.CheckpointArchiveOptions{}); err == nil {
		t.Fatal("import accepted bytes that did not match the manifest digest")
	}
	if _, err := os.Stat(filepath.Join(registry, "checkpoints", manifest.ID)); !os.IsNotExist(err) {
		t.Fatalf("failed import left a published checkpoint: %v", err)
	}

	manifest.Files[0].Path = "../escape"
	traversing := makeCheckpointArchive(t, manifest, content)
	if _, err := stow.ImportCheckpoint(context.Background(), registry, bytes.NewReader(traversing), stow.CheckpointArchiveOptions{}); err == nil {
		t.Fatal("import accepted a traversing manifest path")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(registry), "escape")); !os.IsNotExist(err) {
		t.Fatalf("traversing import wrote outside registry: %v", err)
	}
}

func makeCheckpointArchive(t *testing.T, manifest stow.CheckpointManifest, file []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	writer := tar.NewWriter(gz)
	header := struct {
		FormatVersion int                     `json:"format_version"`
		Manifest      stow.CheckpointManifest `json:"manifest"`
	}{FormatVersion: 1, Manifest: manifest}
	data, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) > 0 {
		if err := writer.WriteHeader(&tar.Header{Name: "files/" + manifest.Files[0].Path, Mode: 0o600, Size: int64(len(file)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func writeTaskFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
