package stow_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestWorkspaceReaderRootsNarrowFilesAndDirectoryMetadata(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	for path, body := range map[string]string{
		"inputs/sub/a.txt":          "allowed",
		"inputs/sibling.txt":        "hidden",
		"inputs-private/secret.txt": "hidden",
		"other/secret.txt":          "hidden",
	} {
		full := filepath.Join(ws.Dir(), filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), ReadRoots: []string{"inputs/sub"}})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.ReadFile(context.Background(), "inputs/sub/a.txt"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"inputs/sibling.txt", "inputs-private/secret.txt", "other/secret.txt"} {
		if _, err := reader.ReadFile(context.Background(), path); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
			t.Errorf("ReadFile(%q) error = %v", path, err)
		}
	}
	assertWorkspaceReaderDirectoryNames(t, reader, ".", []string{"inputs"})
	assertWorkspaceReaderDirectoryNames(t, reader, "inputs", []string{"sub"})
	assertWorkspaceReaderDirectoryNames(t, reader, "inputs/sub", []string{"a.txt"})
	if _, err := reader.ReadDirectory(context.Background(), "other"); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
		t.Fatalf("unrelated directory browse error = %v", err)
	}
}

func TestWorkspaceReaderRootsFilterInvalidUTF8Sibling(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	if err := os.MkdirAll(filepath.Join(ws.Dir(), "inputs", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "inputs", "sub", "a.txt"), []byte("allowed"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidSibling := filepath.Join(ws.Dir(), "inputs", string([]byte{0xff}))
	if err := os.WriteFile(invalidSibling, []byte("outside scope"), 0o600); err != nil {
		t.Skipf("filesystem cannot create invalid UTF-8 names: %v", err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), ReadRoots: []string{"inputs/sub"}})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertWorkspaceReaderDirectoryNames(t, reader, "inputs", []string{"sub"})
}

func assertWorkspaceReaderDirectoryNames(t *testing.T, reader *stow.WorkspaceReader, path string, want []string) {
	t.Helper()
	entries, err := reader.ReadDirectory(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		t.Fatalf("ReadDirectory(%q) entries = %+v, want %v", path, entries, want)
	}
	for index, name := range want {
		if entries[index].Name != name {
			t.Fatalf("ReadDirectory(%q) entry %d = %q, want %q", path, index, entries[index].Name, name)
		}
	}
}

func TestWorkspaceReaderReadRootsRejectInvalidPrivateAndSymlinkPaths(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	if err := os.MkdirAll(filepath.Join(ws.Dir(), "safe", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(ws.Dir(), ".env"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws.Dir(), "safe"), filepath.Join(ws.Dir(), "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	invalid := [][]string{
		{}, {"../safe"}, {"missing"}, {".env"}, {".ſtow"}, {"link"},
	}
	for _, roots := range invalid {
		if _, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), ReadRoots: roots}); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
			t.Errorf("OpenWorkspaceReader(ReadRoots=%q) error = %v", roots, err)
		}
	}
	duplicates := make([]string, 17)
	for index := range duplicates {
		duplicates[index] = "."
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), ReadRoots: duplicates})
	if err != nil {
		t.Fatalf("duplicate roots should count once: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	distinct := make([]string, 17)
	for index := range distinct {
		distinct[index] = fmt.Sprintf("root-%02d", index)
		if err := os.Mkdir(filepath.Join(ws.Dir(), distinct[index]), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), ReadRoots: distinct}); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
		t.Fatalf("17 distinct roots error = %v", err)
	}
}

func TestWorkspaceReaderReadRootsDeduplicateAndSort(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	for _, directory := range []string{"b", "a"} {
		if err := os.Mkdir(filepath.Join(ws.Dir(), directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), ReadRoots: []string{"b", "a", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertWorkspaceReaderDirectoryNames(t, reader, ".", []string{"a", "b"})
}
