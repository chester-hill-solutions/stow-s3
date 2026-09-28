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

// A handoff is the answer to "on what machine", and an answer that only works on
// the machine that wrote it is not one. This is the other machine: a fresh
// registry, a fresh root, nothing but the document and the archive beside it.
func TestWorkspaceAdoptMaterialisesAWorkspaceFromAPortableHandoff(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, _ := seeded(t, registry, filepath.Join(t.TempDir(), "workspace"), "TASK.md", "do the thing")
	edit(t, ws, "TASK.md", "do the other thing")
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}

	// The handoff command resumes the workspace, and a resume takes the session
	// lock, so the handle that seeded it is released first. That is the existing
	// rule for every workspace command, not something the handoff adds.
	if err := ws.Close(); err != nil {
		t.Fatalf("close workspace: %v", err)
	}
	pair := t.TempDir()
	archive := filepath.Join(pair, "checkpoint.tar.gz")
	handoffPath := filepath.Join(pair, "handoff.json")
	if err := handoffWorkspaceCommand([]string{"--id", ws.ID(), "--registry-dir", registry, "--checkpoint-id", checkpoint.ID, "--archive", archive, "--output", handoffPath}); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	// The document is read from the file, because the file is the artifact: what
	// the receiving machine gets is the file, not this process's stdout.
	document := readHandoff(t, handoffPath)
	if document.Version != handoffPortableVersion || document.Archive == nil || document.Archive.SHA256 == "" {
		t.Fatalf("handoff document = %+v", document)
	}
	// The archive travels as the reference names it, so a relative path is what
	// the pair is actually moved around with.
	rewriteArchiveAsRelative(t, handoffPath, filepath.Base(archive))

	receiverRegistry := filepath.Join(t.TempDir(), "receiver-registry")
	root := filepath.Join(t.TempDir(), "adopted")
	output := captureWorkspaceCommand(t, func() error {
		return adoptHandoffCommand([]string{"--handoff", handoffPath, "--root", root, "--registry-dir", receiverRegistry})
	})
	var adopted workspaceResult
	if err := json.Unmarshal(output, &adopted); err != nil {
		t.Fatalf("adopt command response = %s: %v", output, err)
	}
	if adopted.WorkspaceID == ws.ID() {
		t.Error("adopted workspace reused the originating id; a new machine should get its own identity")
	}
	if adopted.CheckpointID != checkpoint.ID {
		t.Errorf("adopted checkpoint = %q, want %q", adopted.CheckpointID, checkpoint.ID)
	}
	body, err := os.ReadFile(filepath.Join(root, "TASK.md"))
	if err != nil {
		t.Fatalf("adopted workspace is missing its file: %v", err)
	}
	if string(body) != "do the other thing" {
		t.Errorf("adopted file = %q, want the handed-off bytes", body)
	}
}

// A handoff that names bytes it cannot vouch for stops before it creates a
// directory. The digest is the whole contract: it is what makes a document
// received over a channel worth trusting.
func TestWorkspaceAdoptRefusesAnArchiveThatNoLongerMatchesItsDigest(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, _ := seeded(t, registry, filepath.Join(t.TempDir(), "workspace"), "TASK.md", "original")
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if err := ws.Close(); err != nil {
		t.Fatalf("close workspace: %v", err)
	}
	pair := t.TempDir()
	archive := filepath.Join(pair, "checkpoint.tar.gz")
	handoffPath := filepath.Join(pair, "handoff.json")
	if err := handoffWorkspaceCommand([]string{"--id", ws.ID(), "--registry-dir", registry, "--checkpoint-id", checkpoint.ID, "--archive", archive, "--output", handoffPath}); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	corrupt(t, archive)

	root := filepath.Join(t.TempDir(), "adopted")
	err = adoptHandoffCommand([]string{"--handoff", handoffPath, "--root", root, "--registry-dir", filepath.Join(t.TempDir(), "registry")})
	if err == nil {
		t.Fatal("adopted a handoff whose archive no longer matched its digest")
	}
	if !strings.Contains(err.Error(), "digest") {
		t.Fatalf("error = %v, want it to name the digest", err)
	}
	if _, statErr := os.Stat(root); statErr == nil {
		t.Fatal("a refused adopt created the workspace root anyway")
	}
}

// A same-machine reference is a legitimate handoff and says so: it names a
// registry directory and no bytes. Adopting it is a category error, and the
// error should be the one that tells the operator to export an archive.
func TestWorkspaceAdoptRefusesASameMachineReference(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, _ := seeded(t, registry, filepath.Join(t.TempDir(), "workspace"), "TASK.md", "task")
	if err := ws.Close(); err != nil {
		t.Fatalf("close workspace: %v", err)
	}
	handoffPath := filepath.Join(t.TempDir(), "handoff.json")
	if err := handoffWorkspaceCommand([]string{"--id", ws.ID(), "--registry-dir", registry, "--output", handoffPath}); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	err := adoptHandoffCommand([]string{"--handoff", handoffPath, "--root", filepath.Join(t.TempDir(), "adopted")})
	if err == nil || !strings.Contains(err.Error(), "no archive") {
		t.Fatalf("error = %v, want a refusal naming the missing archive", err)
	}
}

// A handoff archive is written, not referenced: the caller named the path, and
// something already there is not this handoff's business to replace.
func TestWorkspaceHandoffRefusesToOverwriteAnExistingArchive(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, _ := seeded(t, registry, filepath.Join(t.TempDir(), "workspace"), "TASK.md", "task")
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if err := ws.Close(); err != nil {
		t.Fatalf("close workspace: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "checkpoint.tar.gz")
	if err := os.WriteFile(archive, []byte("already here"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--id", ws.ID(), "--registry-dir", registry, "--checkpoint-id", checkpoint.ID, "--archive", archive}
	if err := handoffWorkspaceCommand(args); err == nil {
		t.Fatal("handoff overwrote an existing archive")
	}
	contents, err := os.ReadFile(archive)
	if err != nil || string(contents) != "already here" {
		t.Fatalf("existing archive changed after a refused overwrite: %v", err)
	}
}

// A handoff reference is read with the same strictness as a task manifest, so a
// document carrying a field nobody implements is refused rather than half
// believed.
func TestReadHandoffDocumentRejectsUnknownFieldsAndUnsupportedVersions(t *testing.T) {
	cases := map[string]string{
		"unknown field": `{"version":2,"workspace_id":"ws_1","registry_dir":"/tmp/r","archive":{"path":"a","sha256":"abc"},"extra":true}`,
		"no id":         `{"version":1,"registry_dir":"/tmp/r"}`,
		"no registry":   `{"version":1,"workspace_id":"ws_1"}`,
		"no digest":     `{"version":2,"workspace_id":"ws_1","registry_dir":"/tmp/r","archive":{"path":"a"}}`,
		"bad version":   `{"version":9,"workspace_id":"ws_1","registry_dir":"/tmp/r"}`,
		"trailing":      `{"version":1,"workspace_id":"ws_1","registry_dir":"/tmp/r"} {}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "handoff.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readHandoffDocument(path); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
}

func readHandoff(t *testing.T, path string) workspaceHandoff {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read handoff: %v", err)
	}
	var document workspaceHandoff
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode handoff: %v", err)
	}
	return document
}

func rewriteArchiveAsRelative(t *testing.T, handoffPath, base string) {
	t.Helper()
	raw, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatal(err)
	}
	var document workspaceHandoff
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document.Archive.Path = base
	rewritten, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handoffPath, rewritten, 0o600); err != nil {
		t.Fatal(err)
	}
}

// corrupt flips a byte in the middle of the archive, which is what a truncated
// transfer or a tampered file looks like to the digest.
func corrupt(t *testing.T, path string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	middle := len(contents) / 2
	contents[middle] ^= 0xff
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
