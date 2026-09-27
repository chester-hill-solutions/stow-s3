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
		if err := os.RemoveAll(checkpointDir); err != nil {
			return fmt.Errorf("workspace: remove checkpoint %s: %w", entry.Name(), err)
		}
	}
	return nil
}
