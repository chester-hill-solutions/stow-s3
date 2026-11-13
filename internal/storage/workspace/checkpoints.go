package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type checkpointReference struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
}

// ForgetCheckpoints removes only checkpoint directories whose manifest names
// this workspace. It deliberately preserves malformed or unreadable entries.
func (r *Registry) ForgetCheckpoints(workspaceID string) error {
	lock, err := AcquireMutationCapture(r.dir, workspaceID)
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := r.CheckWorkspaceRecoveryHolds(workspaceID); err != nil {
		return err
	}
	if err := r.checkWorkspaceFileSaveBarrier(workspaceID); err != nil {
		return err
	}
	return r.forgetCheckpoints(workspaceID)
}

func (r *Registry) checkWorkspaceFileSaveBarrier(workspaceID string) error {
	entry, found, err := r.Lookup(workspaceID)
	if err != nil || !found {
		return err
	}
	return CheckWorkspaceFileSaveBarrier(entry.Dir, entry.ID, entry.Bucket)
}

func (r *Registry) forgetCheckpoints(workspaceID string) error {
	if err := r.CheckWorkspaceRecoveryHolds(workspaceID); err != nil {
		return err
	}
	root := filepath.Join(r.dir, "checkpoints")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("workspace: list checkpoints for cleanup: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		checkpointDir := filepath.Join(root, entry.Name())
		manifestPath := filepath.Join(checkpointDir, "manifest.json")
		info, err := os.Lstat(manifestPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var reference checkpointReference
		if json.Unmarshal(data, &reference) != nil || reference.ID != entry.Name() || reference.WorkspaceID != workspaceID {
			continue
		}
		if err := removeCheckpointDirectory(r.dir, entry.Name(), checkpointDir); err != nil {
			return err
		}
	}
	return syncRegistryDirectory(root)
}

func removeCheckpointDirectory(registryDir, checkpointID, checkpointDir string) error {
	lock, err := AcquireCheckpointRemoval(registryDir, checkpointID)
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := os.RemoveAll(checkpointDir); err != nil {
		return fmt.Errorf("workspace: remove checkpoint %s: %w", checkpointID, err)
	}
	return nil
}

// RemoveWorkspace holds exclusion until workspace bytes, checkpoints and identity are gone.
func (r *Registry) RemoveWorkspace(id string, remove func() error) error {
	lock, err := AcquireMutationCapture(r.dir, id)
	if err != nil {
		return err
	}
	defer lock.Release()
	attachment, err := acquireAttachmentRemoval(r.dir, id)
	if err != nil {
		return err
	}
	defer attachment.Release()
	if err := r.CheckWorkspaceRecoveryHolds(id); err != nil {
		return err
	}
	if err := r.checkWorkspaceFileSaveBarrier(id); err != nil {
		return err
	}
	if err := remove(); err != nil {
		return err
	}
	if err := r.forgetCheckpoints(id); err != nil {
		return err
	}
	return r.forget(id)
}

// ValidWorkspaceID accepts portable identifiers without path syntax.
func ValidWorkspaceID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
