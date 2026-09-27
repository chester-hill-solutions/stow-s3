package stow_test

import (
	"os"
	"os/exec"
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

func TestPrepareWorkspaceClonesAnExplicitGitRefWithoutTouchingSource(t *testing.T) {
	git, source, parentCommit, commit := newPreparedGitSource(t)
	writeGitTestFile(t, source, "README.md", "dirty source")
	writeGitTestFile(t, source, "untracked.txt", "source only")
	sourceStatus := gitTestCommand(t, git, source, "status", "--porcelain")
	worktreesBefore := gitTestCommand(t, git, source, "worktree", "list", "--porcelain")

	root := filepath.Join(t.TempDir(), "task")
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
		WorkingDirectory: "repo",
		Repositories: []stow.GitRepositoryInput{{
			Source: source, Destination: "repo", Ref: "refs/heads/main",
		}},
	})
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	defer prepared.Workspace.Close()
	assertPreparedGitCheckout(t, git, prepared, parentCommit, commit)
	assertGitSourceUnchanged(t, git, source, sourceStatus, worktreesBefore)
	if prepared.SeededObjects == 0 || prepared.SeededBytes == 0 {
		t.Fatalf("Git seed totals = (%d bytes, %d objects)", prepared.SeededBytes, prepared.SeededObjects)
	}
}

func newPreparedGitSource(t *testing.T) (git, source, parentCommit, commit string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	source = filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, git, source, "init", "-b", "main")
	gitTestCommand(t, git, source, "config", "user.name", "Stow Test")
	gitTestCommand(t, git, source, "config", "user.email", "stow-test@example.invalid")
	writeGitTestFile(t, source, "README.md", "old version")
	gitTestCommand(t, git, source, "add", "README.md")
	gitTestCommand(t, git, source, "commit", "-m", "baseline")
	parentCommit = gitTestCommand(t, git, source, "rev-parse", "HEAD")
	writeGitTestFile(t, source, "README.md", "committed version")
	gitTestCommand(t, git, source, "commit", "-am", "task base")
	commit = gitTestCommand(t, git, source, "rev-parse", "HEAD")
	return git, source, parentCommit, commit
}

func writeGitTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertPreparedGitCheckout(t *testing.T, git string, prepared *stow.PreparedWorkspace, parentCommit, commit string) {
	t.Helper()
	checkout := prepared.WorkingDirectory
	if got := gitTestCommand(t, git, checkout, "rev-parse", "HEAD"); got != commit {
		t.Fatalf("checked out commit = %s, want %s", got, commit)
	}
	if got, err := os.ReadFile(filepath.Join(checkout, "README.md")); err != nil || string(got) != "committed version" {
		t.Fatalf("checked out README = %q, %v", got, err)
	}
	if got := gitTestCommand(t, git, checkout, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("task repository history count = %s, want only the selected commit", got)
	}
	command := exec.Command(git, "-C", checkout, "cat-file", "-e", parentCommit+"^{commit}")
	if command.Run() == nil {
		t.Fatal("task repository contains history before the selected ref")
	}
	if got := gitTestCommand(t, git, checkout, "remote"); got != "" {
		t.Fatalf("task repository unexpectedly has a source remote: %q", got)
	}
	if _, err := os.Stat(filepath.Join(checkout, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked source file leaked into task repository: %v", err)
	}
	if len(prepared.Repositories) != 1 || prepared.Repositories[0].Commit != commit {
		t.Fatalf("prepared repository identity = %+v", prepared.Repositories)
	}
}

func assertGitSourceUnchanged(t *testing.T, git, source, status, worktrees string) {
	t.Helper()
	if got := gitTestCommand(t, git, source, "status", "--porcelain"); got != status {
		t.Fatalf("source checkout status changed: before %q, after %q", status, got)
	}
	if got := gitTestCommand(t, git, source, "worktree", "list", "--porcelain"); got != worktrees {
		t.Fatalf("source worktree registrations changed:\nbefore %s\nafter %s", worktrees, got)
	}
}

func TestPrepareWorkspaceRejectsUnknownGitRefAndCleansRoot(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	source := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, git, source, "init", "-b", "main")
	gitTestCommand(t, git, source, "config", "user.name", "Stow Test")
	gitTestCommand(t, git, source, "config", "user.email", "stow-test@example.invalid")
	if err := os.WriteFile(filepath.Join(source, "task.txt"), []byte("task"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, git, source, "add", "task.txt")
	gitTestCommand(t, git, source, "commit", "-m", "baseline")
	root := filepath.Join(t.TempDir(), "task")
	_, err = stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
		Repositories:     []stow.GitRepositoryInput{{Source: source, Destination: "repo", Ref: "refs/heads/missing"}},
	})
	if err == nil {
		t.Fatal("PrepareWorkspace accepted an unknown Git ref")
	}
	if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
		t.Fatalf("failed Git prepare left an owned partial root: %v", statErr)
	}
}

func gitTestCommand(t *testing.T, git, directory string, args ...string) string {
	t.Helper()
	command := exec.Command(git, append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
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

func TestPrepareWorkspaceRejectsNonPortableDestinationSegments(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(source, []byte("task"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"CON/output.txt", "folder./output.txt", "bad?/output.txt"} {
		t.Run(destination, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "task")
			_, err := stow.PrepareWorkspace(stow.PrepareOptions{
				WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry")},
				Inputs:           []stow.WorkspaceInput{{Source: source, Destination: destination}},
			})
			if err == nil {
				t.Fatalf("PrepareWorkspace accepted non-portable destination %q", destination)
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
