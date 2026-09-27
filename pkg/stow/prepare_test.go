package stow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestPrepareWorkspaceCopiesInputsIntoOwnedWorkingDirectory(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "fixtures", "input.txt"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "task")
	registry := filepath.Join(t.TempDir(), "registry")
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: registry},
		WorkingDirectory: "project",
		Inputs:           []stow.WorkspaceInput{{Source: filepath.Join(source, "fixtures"), Destination: "project/testdata"}},
	})
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	defer prepared.Workspace.Close()
	if !prepared.Workspace.Capabilities().Persistent {
		t.Fatal("prepared workspace is not persistent")
	}
	if prepared.Workspace.Authority().Allows(stow.UpstreamRead) || prepared.Workspace.Authority().Allows(stow.UpstreamWrite) {
		t.Fatal("prepared local task inherited upstream authority")
	}
	if got, err := os.ReadFile(filepath.Join(prepared.WorkingDirectory, "testdata", "input.txt")); err != nil || string(got) != "fixture" {
		t.Fatalf("seeded file = %q, %v", got, err)
	}
	if prepared.SeededBytes != int64(len("fixture")) || prepared.SeededObjects != 1 {
		t.Fatalf("seed totals = (%d bytes, %d objects)", prepared.SeededBytes, prepared.SeededObjects)
	}
	if !strings.HasPrefix(prepared.BaseIdentity, "sha256:") {
		t.Fatalf("base identity = %q, want SHA-256 fingerprint", prepared.BaseIdentity)
	}
	if _, err := os.Stat(filepath.Join(source, "fixtures", "input.txt")); err != nil {
		t.Fatalf("source input was modified or removed: %v", err)
	}
	assertPreparedWorkingDirectoryResumes(t, prepared.Workspace, registry, prepared.WorkingDirectory)
}

func assertPreparedWorkingDirectoryResumes(t *testing.T, ws *stow.Workspace, registry, workingDirectory string) {
	t.Helper()
	id := ws.ID()
	if err := ws.Close(); err != nil {
		t.Fatalf("close prepared workspace: %v", err)
	}
	resumed, err := stow.ResumeIn(registry, id)
	if err != nil {
		t.Fatalf("resume prepared workspace: %v", err)
	}
	defer resumed.Close()
	if resumed.WorkingDirectory() != workingDirectory {
		t.Fatalf("resumed working directory = %q, want %q", resumed.WorkingDirectory(), workingDirectory)
	}
}

func TestPrepareWorkspaceRejectsTraversalAndCleansItsOwnedPartialRoot(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "task")
	_, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
		Inputs:           []stow.WorkspaceInput{{Source: source, Destination: "repo/../escape"}},
	})
	if err == nil {
		t.Fatal("PrepareWorkspace accepted traversal")
	}
	if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
		t.Fatalf("failed prepare left a partial owned root: %v", statErr)
	}
}

func TestPrepareWorkspaceRejectsSymlinkInputsAndCollisions(t *testing.T) {
	sourceDir := t.TempDir()
	file := filepath.Join(sourceDir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sourceDir, "link.txt")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, tc := range []struct {
		name   string
		inputs []stow.WorkspaceInput
	}{
		{name: "symlink", inputs: []stow.WorkspaceInput{{Source: link, Destination: "link.txt"}}},
		{name: "collision", inputs: []stow.WorkspaceInput{{Source: file, Destination: "same.txt"}, {Source: file, Destination: "same.txt"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "task")
			_, err := stow.PrepareWorkspace(stow.PrepareOptions{
				WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
				Inputs:           tc.inputs,
			})
			if err == nil {
				t.Fatal("PrepareWorkspace accepted unsafe input")
			}
			if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
				t.Fatalf("failed prepare left partial root: %v", statErr)
			}
		})
	}
}

func TestPrepareWorkspaceRequiresExplicitSensitiveInputOptIn(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, ".env.local"), []byte("TOKEN=example"), 0o600); err != nil {
		t.Fatal(err)
	}
	makeOptions := func(root string, include bool) stow.PrepareOptions {
		return stow.PrepareOptions{
			WorkspaceOptions:       stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
			Inputs:                 []stow.WorkspaceInput{{Source: source, Destination: "repo"}},
			IncludeSensitiveInputs: include,
		}
	}
	root := filepath.Join(t.TempDir(), "denied")
	if _, err := stow.PrepareWorkspace(makeOptions(root, false)); err == nil {
		t.Fatal("PrepareWorkspace copied sensitive-looking file without opt-in")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("rejected prepare left a partial root: %v", err)
	}
	included, err := stow.PrepareWorkspace(makeOptions(filepath.Join(t.TempDir(), "included"), true))
	if err != nil {
		t.Fatalf("explicit sensitive-input opt-in: %v", err)
	}
	defer included.Workspace.Close()
}
