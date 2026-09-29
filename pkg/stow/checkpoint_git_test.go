package stow_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestCheckpointGitReconstructionPreservesDeletion(t *testing.T) {
	source := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git(source, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(source, "gone.txt"), []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "edited.txt"), []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	git(source, "add", ".")
	git(source, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	commit := git(source, "rev-parse", "HEAD")
	registry := t.TempDir()
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry}, WorkingDirectory: "repo", Repositories: []stow.GitRepositoryInput{{Source: source, Destination: "repo", Ref: commit}}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Workspace.Close()
	root := prepared.Workspace.Dir()
	if err := os.Remove(filepath.Join(root, "repo", "gone.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "repo", "edited.txt"), []byte("agent"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "repo", "new.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	cp, err := prepared.Workspace.CreateCheckpoint(context.Background(), stow.CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "continued")
	restored, err := stow.RestoreCheckpointWithGit(registry, cp.ID, stow.WorkspaceOptions{Dir: destination, RegistryDir: registry}, []stow.GitRepositoryInput{{Source: source, Destination: "repo"}})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got := git(filepath.Join(destination, "repo"), "rev-parse", "HEAD"); got != commit {
		t.Fatalf("HEAD=%s", got)
	}
	status := git(filepath.Join(destination, "repo"), "status", "--short")
	for _, want := range []string{"D gone.txt", "M edited.txt", "?? new.txt"} {
		if !strings.Contains(status, want) {
			t.Fatalf("missing %q in status %q", want, status)
		}
	}
	if _, err := os.Stat(filepath.Join(destination, "repo", "gone.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file resurrected: %v", err)
	}
}
