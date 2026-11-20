package stow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func sensitiveSource(t *testing.T) string {
	t.Helper()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "note.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestPrepareSkipsSensitiveInputsByDefaultAndReportsThem(t *testing.T) {
	root := filepath.Join(t.TempDir(), "task")
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
		Inputs:           []stow.WorkspaceInput{{Source: sensitiveSource(t), Destination: "seed"}},
	})
	if err != nil {
		t.Fatalf("prepare with a .env: %v", err)
	}
	defer prepared.Workspace.Close()
	if _, err := os.Stat(filepath.Join(root, "seed", ".env")); !os.IsNotExist(err) {
		t.Fatalf(".env was copied: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "seed", "note.txt")); err != nil || string(data) != "keep" {
		t.Fatalf("ordinary input = %q, %v", data, err)
	}
	if len(prepared.Excluded) != 1 || prepared.Excluded[0] != "seed/.env" {
		t.Fatalf("excluded = %v", prepared.Excluded)
	}
	if prepared.SeededObjects != 1 {
		t.Fatalf("seeded objects = %d, want only the ordinary file", prepared.SeededObjects)
	}
}

func TestPrepareCopiesSensitiveInputsWhenAsked(t *testing.T) {
	root := filepath.Join(t.TempDir(), "task")
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions:       stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
		Inputs:                 []stow.WorkspaceInput{{Source: sensitiveSource(t), Destination: "seed"}},
		IncludeSensitiveInputs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Workspace.Close()
	if data, err := os.ReadFile(filepath.Join(root, "seed", ".env")); err != nil || string(data) != "SECRET=1" {
		t.Fatalf("included .env = %q, %v", data, err)
	}
	if len(prepared.Excluded) != 0 {
		t.Fatalf("excluded = %v", prepared.Excluded)
	}
}

func TestPrepareCanRefuseSensitiveInputsInsteadOfSkipping(t *testing.T) {
	root := filepath.Join(t.TempDir(), "task")
	_, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
		Inputs:           []stow.WorkspaceInput{{Source: sensitiveSource(t), Destination: "seed"}},
		SensitivePaths:   "refuse",
	})
	if err == nil || !strings.Contains(err.Error(), "sensitive-looking") {
		t.Fatalf("refuse policy = %v", err)
	}
	if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
		t.Fatalf("refused prepare left a workspace: %v", statErr)
	}
}

func TestPrepareRejectsUnknownSensitivePolicy(t *testing.T) {
	_, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: filepath.Join(t.TempDir(), "registry")},
		Inputs:           []stow.WorkspaceInput{{Source: sensitiveSource(t), Destination: "seed"}},
		SensitivePaths:   "sometimes",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown sensitive_paths") {
		t.Fatalf("unknown policy = %v", err)
	}
}

func TestPrepareSkipsSensitiveGitInputsAndDoesNotLeaveThemCheckedOut(t *testing.T) {
	git, source, _, _ := newPreparedGitSource(t)
	writeGitTestFile(t, source, ".env", "SECRET=1")
	gitTestCommand(t, git, source, "add", ".env")
	gitTestCommand(t, git, source, "commit", "-m", "add secret")

	root := filepath.Join(t.TempDir(), "task")
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
		Repositories:     []stow.GitRepositoryInput{{Source: source, Destination: "repo", Ref: "HEAD"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Workspace.Close()
	if _, err := os.Stat(filepath.Join(root, "repo", ".env")); !os.IsNotExist(err) {
		t.Fatalf("Git .env was left in the workspace: %v", err)
	}
	if len(prepared.Excluded) != 1 || prepared.Excluded[0] != "repo/.env" {
		t.Fatalf("excluded = %v", prepared.Excluded)
	}
}
