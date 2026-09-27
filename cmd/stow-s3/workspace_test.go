package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestReadWorkspaceManifestResolvesLocalPaths(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "task.json")
	input := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(input, []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestJSON := `{"version":1,"root":"../tasks/work","working_directory":"repo","inputs":[{"source":"input.txt","destination":"repo/TASK.md"}],"repositories":[{"source":"../source-repo","destination":"repo","ref":"refs/heads/main"}],"registry_dir":"../registry"}`
	if err := os.WriteFile(manifestPath, []byte(manifestJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := readWorkspaceManifest(manifestPath)
	if err != nil {
		t.Fatalf("readWorkspaceManifest: %v", err)
	}
	if manifest.Root != filepath.Join(dir, "../tasks/work") {
		t.Fatalf("root = %q", manifest.Root)
	}
	if manifest.Inputs[0].Source != input {
		t.Fatalf("input source = %q, want %q", manifest.Inputs[0].Source, input)
	}
	if manifest.Repositories[0].Source != filepath.Join(dir, "../source-repo") || manifest.Repositories[0].Ref != "refs/heads/main" {
		t.Fatalf("repository input = %+v", manifest.Repositories[0])
	}
	if manifest.RegistryDir != filepath.Join(dir, "../registry") {
		t.Fatalf("registry = %q", manifest.RegistryDir)
	}
}

func TestWorkspacePrepareCommandStagesGitRefAndReportsCommit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	workspaceGitTestCommand(t, git, source, "init", "-b", "main")
	workspaceGitTestCommand(t, git, source, "config", "user.name", "Stow Test")
	workspaceGitTestCommand(t, git, source, "config", "user.email", "stow-test@example.invalid")
	if err := os.WriteFile(filepath.Join(source, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitTestCommand(t, git, source, "add", "main.go")
	workspaceGitTestCommand(t, git, source, "commit", "-m", "baseline")
	commit := workspaceGitTestCommand(t, git, source, "rev-parse", "HEAD")
	manifestPath := filepath.Join(base, "task.json")
	manifest := `{"version":1,"root":"task-root","working_directory":"repo","repositories":[{"source":"source","destination":"repo","ref":"refs/heads/main"}],"registry_dir":"registry"}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureWorkspaceCommand(t, func() error {
		return prepareWorkspaceCommand([]string{"--manifest", manifestPath})
	})
	var result workspaceResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("prepare result = %s: %v", output, err)
	}
	if result.WorkingDirectory != filepath.Join(base, "task-root", "repo") || len(result.Repositories) != 1 || result.Repositories[0].Commit != commit {
		t.Fatalf("prepare result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(result.WorkingDirectory, "main.go")); err != nil {
		t.Fatalf("prepared source file missing: %v", err)
	}
}

func workspaceGitTestCommand(t *testing.T, git, directory string, args ...string) string {
	t.Helper()
	command := exec.Command(git, append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestExportCheckpointFileRefusesToOverwrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	registry := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer ws.Close()
	if err := os.WriteFile(filepath.Join(root, "task.txt"), []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "task.tar.gz")
	if err := exportCheckpointFile(registry, checkpoint.ID, archive, stow.CheckpointArchiveOptions{}); err != nil {
		t.Fatalf("export checkpoint: %v", err)
	}
	contents, err := os.ReadFile(archive)
	if err != nil || len(contents) == 0 {
		t.Fatalf("archive = %d bytes, %v", len(contents), err)
	}
	if err := exportCheckpointFile(registry, checkpoint.ID, archive, stow.CheckpointArchiveOptions{}); err == nil {
		t.Fatal("export overwrote an existing archive")
	}
	contentsAfter, err := os.ReadFile(archive)
	if err != nil || string(contentsAfter) != string(contents) {
		t.Fatalf("existing archive changed after refused overwrite: %v", err)
	}
}

func TestWorkspaceExportAndImportCommands(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "source-registry")
	workspace, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer workspace.Close()
	if err := os.WriteFile(filepath.Join(workspace.Dir(), "TASK.md"), []byte("fix this"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := workspace.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "handoff.tar.gz")
	output := captureWorkspaceCommand(t, func() error {
		return exportCheckpointCommand([]string{"--checkpoint-id", checkpoint.ID, "--registry-dir", registry, "--output", archive})
	})
	var exported struct {
		CheckpointID string `json:"checkpoint_id"`
	}
	if err := json.Unmarshal(output, &exported); err != nil || exported.CheckpointID != checkpoint.ID {
		t.Fatalf("export command response = %s, %v", output, err)
	}
	destinationRegistry := filepath.Join(t.TempDir(), "destination-registry")
	output = captureWorkspaceCommand(t, func() error {
		return importCheckpointCommand([]string{"--archive", archive, "--registry-dir", destinationRegistry})
	})
	var imported checkpointResult
	if err := json.Unmarshal(output, &imported); err != nil || imported.ID != checkpoint.ID || imported.Files != 1 {
		t.Fatalf("import command response = %s, %v", output, err)
	}
}

func captureWorkspaceCommand(t *testing.T, command func() error) []byte {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	commandErr := command()
	_ = writer.Close()
	os.Stdout = previous
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if commandErr != nil {
		t.Fatalf("workspace command: %v", commandErr)
	}
	if readErr != nil {
		t.Fatalf("read workspace command output: %v", readErr)
	}
	return output
}

func TestReadWorkspaceManifestRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	for name, content := range map[string]string{
		"unknown":  `{"version":1,"root":"../task","inputs":[],"credential":"ambient"}`,
		"trailing": `{"version":1,"root":"../task","inputs":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "task.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readWorkspaceManifest(path); err == nil {
				t.Fatal("accepted malformed task manifest")
			}
		})
	}
}
