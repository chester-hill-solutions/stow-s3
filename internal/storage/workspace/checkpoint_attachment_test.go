package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointAttachmentsShareLeaseAndExcludeRemoval(t *testing.T) {
	requireAttachmentLocks(t)
	registryDir := t.TempDir()
	checkpointID := "cp_0123456789abcdef01234567"
	first, err := AcquireCheckpointReadAttachment(registryDir, checkpointID)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := AcquireCheckpointReadAttachment(registryDir, checkpointID)
	if err != nil {
		t.Fatalf("second shared checkpoint attachment: %v", err)
	}
	defer second.Release()
	if _, err := AcquireCheckpointRemoval(registryDir, checkpointID); !errors.Is(err, ErrCheckpointInUse) {
		t.Fatalf("checkpoint removal error = %v, want ErrCheckpointInUse", err)
	}
	capture, err := AcquireCapture(registryDir, "ws-checkpoint-lock")
	if err != nil {
		t.Fatalf("checkpoint attachment must not block capture: %v", err)
	}
	defer capture.Release()
}

func TestCheckpointAttachmentSetupFailureCleansUp(t *testing.T) {
	requireAttachmentLocks(t)
	registryDir := t.TempDir()
	checkpointID := "cp_0123456789abcdef01234567"
	lockDir := filepath.Join(registryDir, ".stow", "checkpoint-locks")
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireCheckpointReadAttachment(registryDir, checkpointID); err == nil {
		t.Fatal("checkpoint attachment setup unexpectedly succeeded")
	}
	if err := os.Remove(lockDir); err != nil {
		t.Fatal(err)
	}
	lease, err := AcquireCheckpointReadAttachment(registryDir, checkpointID)
	if err != nil {
		t.Fatalf("attachment after setup failure: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	removal, err := AcquireCheckpointRemoval(registryDir, checkpointID)
	if err != nil {
		t.Fatalf("removal after reader release: %v", err)
	}
	if err := removal.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestForgetCheckpointsRefusesAttachedCheckpoint(t *testing.T) {
	requireAttachmentLocks(t)
	registry, registryDir, _ := attachmentFixture(t)
	checkpointID := "cp_0123456789abcdef01234567"
	checkpointDir := filepath.Join(registryDir, "checkpoints", checkpointID)
	if err := os.MkdirAll(checkpointDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(checkpointReference{ID: checkpointID, WorkspaceID: "ws-attachment"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkpointDir, "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	lease, err := AcquireCheckpointReadAttachment(registryDir, checkpointID)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ForgetCheckpoints("ws-attachment"); !errors.Is(err, ErrCheckpointInUse) {
		t.Fatalf("bulk checkpoint removal error = %v, want ErrCheckpointInUse", err)
	}
	if _, err := os.Stat(checkpointDir); err != nil {
		t.Fatalf("checkpoint removed while attached: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	if err := registry.ForgetCheckpoints("ws-attachment"); err != nil {
		t.Fatalf("bulk removal after release: %v", err)
	}
	if _, err := os.Stat(checkpointDir); !os.IsNotExist(err) {
		t.Fatalf("checkpoint remains after released attachment: %v", err)
	}
}
