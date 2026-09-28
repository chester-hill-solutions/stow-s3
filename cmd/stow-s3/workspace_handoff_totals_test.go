package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// The adopt response reports how much arrived. Those two numbers are a byte
// count and a file count, and a reader has one way to interpret them: whichever
// is larger is the byte total. Swapping them reports a small file count as
// bytes and a large byte total as files, which is not a cosmetic transposition
// — it inverts the only signal a receiver has that the transfer carried what
// they expected.
//
// prepare reports the same two fields through the same constructor and gets
// them right, so the shape is proven and only this call site was wrong.
func TestWorkspaceAdoptReportsTheTransferredTotalsInTheRightFields(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, _ := seeded(t, registry, filepath.Join(t.TempDir(), "workspace"), "TASK.md", "0123456789")
	if err := ws.Close(); err != nil {
		t.Fatalf("close workspace: %v", err)
	}
	checkpoint, err := stow.CheckpointOf(context.Background(), registry, ws.ID(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CheckpointOf: %v", err)
	}

	pair := t.TempDir()
	archive := filepath.Join(pair, "checkpoint.tar.gz")
	handoffPath := filepath.Join(pair, "handoff.json")
	if err := handoffWorkspaceCommand([]string{"--id", ws.ID(), "--registry-dir", registry, "--checkpoint-id", checkpoint.ID, "--archive", archive, "--output", handoffPath}); err != nil {
		t.Fatalf("handoff: %v", err)
	}

	output := captureWorkspaceCommand(t, func() error {
		return adoptHandoffCommand([]string{
			"--handoff", handoffPath,
			"--root", filepath.Join(t.TempDir(), "adopted"),
			"--registry-dir", filepath.Join(t.TempDir(), "receiver-registry"),
		})
	})
	var adopted workspaceResult
	if err := json.Unmarshal(output, &adopted); err != nil {
		t.Fatalf("adopt response = %s: %v", output, err)
	}

	// The checkpoint itself is the authority on what it carried, so the
	// assertion is against that rather than against a literal.
	if adopted.SeededObjects != checkpoint.Files {
		t.Errorf("seeded_objects = %d, want the checkpoint's file count %d", adopted.SeededObjects, checkpoint.Files)
	}
	if adopted.SeededBytes != checkpoint.Bytes {
		t.Errorf("seeded_bytes = %d, want the checkpoint's byte total %d", adopted.SeededBytes, checkpoint.Bytes)
	}
	if adopted.SeededBytes < adopted.SeededObjects {
		t.Errorf("seeded_bytes (%d) < seeded_objects (%d): a byte total is smaller than its own file count, so the fields are transposed",
			adopted.SeededBytes, adopted.SeededObjects)
	}
}
