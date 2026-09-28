package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// seeded opens a workspace holding one file and returns it with its first
// checkpoint, which is the base every delta in these tests is measured from.
func seeded(t *testing.T, registry, root, name, body string) (*stow.Workspace, string) {
	t.Helper()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	return ws, checkpoint.ID
}

func edit(t *testing.T, ws *stow.Workspace, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws.Dir(), name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertCheckpointFile(t *testing.T, registry, checkpointID, path, want string) {
	t.Helper()
	body, err := stow.ReadCheckpointFile(registry, checkpointID, path)
	if err != nil {
		t.Fatalf("ReadCheckpointFile(%s, %q): %v", checkpointID, path, err)
	}
	if string(body) != want {
		t.Fatalf("%s in %s = %q, want %q", path, checkpointID, body, want)
	}
}

// The flow this exists for: one machine holds the base, another holds the
// change, and the change costs what the difference costs. The delta is written
// here, the base is carried over as an archive, and the delta is applied there.
func TestWorkspaceDeltaAppliesOnTheMachineThatOnlyHasTheBase(t *testing.T) {
	originRegistry := filepath.Join(t.TempDir(), "origin-registry")
	ws, base := seeded(t, originRegistry, filepath.Join(t.TempDir(), "workspace"), "edit.txt", "before")
	edit(t, ws, "edit.txt", "after")
	edit(t, ws, "added.txt", "new file")
	target, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}

	deltaPath := filepath.Join(t.TempDir(), "change.stowdelta")
	output := captureWorkspaceCommand(t, func() error {
		return deltaWorkspaceCommand([]string{"--from", base, "--to", target.ID, "--registry-dir", originRegistry, "--output", deltaPath})
	})
	var described deltaResult
	if err := json.Unmarshal(output, &described); err != nil {
		t.Fatalf("delta command response = %s: %v", output, err)
	}
	if described.BaseID != base || described.TargetID != target.ID || described.SHA256 == "" {
		t.Fatalf("delta command response = %+v", described)
	}
	if _, err := os.Stat(deltaPath); err != nil {
		t.Fatalf("delta document missing: %v", err)
	}

	// The other machine already has the base and nothing else.
	receiverRegistry := filepath.Join(t.TempDir(), "receiver-registry")
	archive := filepath.Join(t.TempDir(), "base.tar.gz")
	if err := exportCheckpointFile(originRegistry, base, archive, stow.CheckpointArchiveOptions{}); err != nil {
		t.Fatalf("export base: %v", err)
	}
	archiveFile, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stow.ImportCheckpoint(context.Background(), receiverRegistry, archiveFile, stow.CheckpointArchiveOptions{}); err != nil {
		t.Fatalf("import base: %v", err)
	}
	_ = archiveFile.Close()

	output = captureWorkspaceCommand(t, func() error {
		return applyDeltaCommand([]string{"--delta", deltaPath, "--base", base, "--registry-dir", receiverRegistry})
	})
	var applied checkpointResult
	if err := json.Unmarshal(output, &applied); err != nil {
		t.Fatalf("apply command response = %s: %v", output, err)
	}
	if applied.ID == "" || applied.WorkspaceID != ws.ID() {
		t.Fatalf("apply command response = %+v", applied)
	}
	assertCheckpointFile(t, receiverRegistry, applied.ID, "edit.txt", "after")
	assertCheckpointFile(t, receiverRegistry, applied.ID, "added.txt", "new file")

	// The base is a real point, not a scratch pad: applying a delta leaves it be.
	assertCheckpointFile(t, receiverRegistry, base, "edit.txt", "before")
}

// A delta applied to a point that is already the target is a conflict, and the
// refusal has to name itself: the operator's next move is to re-measure from the
// target, and an opaque error does not tell them that.
func TestWorkspaceApplyReportsAConflict(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, base := seeded(t, registry, filepath.Join(t.TempDir(), "workspace"), "edit.txt", "before")
	edit(t, ws, "edit.txt", "after")
	target, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	deltaPath := filepath.Join(t.TempDir(), "change.stowdelta")
	if err := deltaWorkspaceCommand([]string{"--from", base, "--to", target.ID, "--registry-dir", registry, "--output", deltaPath}); err != nil {
		t.Fatalf("delta: %v", err)
	}
	err = applyDeltaCommand([]string{"--delta", deltaPath, "--base", target.ID, "--registry-dir", registry})
	if err == nil {
		t.Fatal("applying a delta to its own target succeeded")
	}
	if !strings.Contains(err.Error(), "diverged") {
		t.Fatalf("error = %v, want a named conflict", err)
	}
	assertCheckpointFile(t, registry, target.ID, "edit.txt", "after")
}

// Every published artifact refuses to overwrite, because a caller may already be
// holding the path it was given.
func TestWorkspaceDeltaRefusesToOverwriteAnExistingDocument(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, base := seeded(t, registry, filepath.Join(t.TempDir(), "workspace"), "a.txt", "one")
	edit(t, ws, "a.txt", "two")
	target, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	deltaPath := filepath.Join(t.TempDir(), "change.stowdelta")
	args := []string{"--from", base, "--to", target.ID, "--registry-dir", registry, "--output", deltaPath}
	if err := deltaWorkspaceCommand(args); err != nil {
		t.Fatalf("delta: %v", err)
	}
	first, err := os.ReadFile(deltaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := deltaWorkspaceCommand(args); err == nil {
		t.Fatal("delta overwrote an existing document")
	}
	after, err := os.ReadFile(deltaPath)
	if err != nil || string(after) != string(first) {
		t.Fatalf("existing document changed after a refused overwrite: %v", err)
	}
}
