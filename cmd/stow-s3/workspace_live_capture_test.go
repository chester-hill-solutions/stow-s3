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

// The workflow: an agent is working in a workspace, and something outside it takes
// a snapshot and writes a handoff reference without the agent being interrupted.
// Neither command may resume the workspace, or both fail with "workspace is in
// use" for as long as the agent is running.
func TestCheckpointAndHandoffWorkWhileAnAgentHoldsTheWorkspace(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	root := filepath.Join(t.TempDir(), "task")
	live, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer live.Close()
	if err := os.WriteFile(filepath.Join(root, "TASK.md"), []byte("work in progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The session is held for the whole test: nothing here may release it.
	if _, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: registry}, live.ID()); err == nil {
		t.Fatal("the workspace was not in use; this test would pass without the change")
	}

	output := captureWorkspaceCommand(t, func() error {
		return checkpointWorkspaceCommand([]string{"--id", live.ID(), "--registry-dir", registry})
	})
	var checkpoint checkpointResult
	if err := json.Unmarshal(output, &checkpoint); err != nil {
		t.Fatalf("checkpoint response = %s: %v", output, err)
	}
	if checkpoint.ID == "" || checkpoint.Files != 1 {
		t.Fatalf("checkpoint response = %+v", checkpoint)
	}
	body, err := stow.ReadCheckpointFile(registry, checkpoint.ID, "TASK.md")
	if err != nil {
		t.Fatalf("captured file: %v", err)
	}
	if string(body) != "work in progress" {
		t.Errorf("captured %q, want the bytes on disk", body)
	}

	// A handoff naming a live workspace, with the bytes, and without disturbing it.
	pair := t.TempDir()
	archive := filepath.Join(pair, "checkpoint.tar.gz")
	handoffPath := filepath.Join(pair, "handoff.json")
	if err := handoffWorkspaceCommand([]string{"--id", live.ID(), "--registry-dir", registry, "--checkpoint-id", checkpoint.ID, "--archive", archive, "--output", handoffPath}); err != nil {
		t.Fatalf("handoff of a live workspace: %v", err)
	}
	document := readHandoff(t, handoffPath)
	if document.CheckpointID != checkpoint.ID || document.Archive == nil {
		t.Fatalf("handoff document = %+v", document)
	}
	if _, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: registry}, live.ID()); err == nil {
		t.Fatal("taking a checkpoint or a handoff reference released the live session")
	}

	// The handoff names a live workspace and the bytes, and the receiving machine
	// materialises them without the agent ever being involved.
	assertHandoffAdopts(t, handoffPath, "work in progress")
}

// assertHandoffAdopts is the far end of the previous test, split out because it is
// a different claim: that a reference produced beside a live session still carries
// everything the receiving side needs.
func assertHandoffAdopts(t *testing.T, handoffPath, want string) {
	t.Helper()
	adopted := filepath.Join(t.TempDir(), "adopted")
	output := captureWorkspaceCommand(t, func() error {
		return adoptHandoffCommand([]string{"--handoff", handoffPath, "--root", adopted, "--registry-dir", filepath.Join(t.TempDir(), "registry")})
	})
	var result workspaceResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("adopt response = %s: %v", output, err)
	}
	body, err := os.ReadFile(filepath.Join(adopted, "TASK.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Errorf("adopted %q, want the captured bytes", body)
	}
}

// The retention caps are recorded in the registry, so a capture from outside the
// workspace enforces the same limits its handle would. A cap that only the handle
// knows about is a cap the orchestrator can walk straight past.
func TestACaptureFromOutsideEnforcesTheWorkspacesRetentionCaps(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
		Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: registry, MaxCheckpoints: 1,
	})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer ws.Close()
	if err := os.WriteFile(filepath.Join(ws.Dir(), "a.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := stow.CheckpointOf(context.Background(), registry, ws.ID(), stow.CheckpointOptions{}); err != nil {
		t.Fatalf("first capture: %v", err)
	}
	_, err = stow.CheckpointOf(context.Background(), registry, ws.ID(), stow.CheckpointOptions{})
	if err == nil {
		t.Fatal("a second capture passed a count cap of one")
	}
	if !strings.Contains(err.Error(), "count limit reached") {
		t.Fatalf("error = %v, want it to name the cap", err)
	}
}
