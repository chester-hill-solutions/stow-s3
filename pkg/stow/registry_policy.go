package stow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type RegistryPolicy = workspace.RegistryPolicy

func GetRegistryPolicy(registryDir string) (RegistryPolicy, error) {
	registry, err := openRegistryReadOnly(registryDir, "")
	if err != nil {
		return RegistryPolicy{}, err
	}
	return registry.Policy()
}

// SetRegistryPolicy persists standing limits; zero is unlimited. Existing usage must fit.
func SetRegistryPolicy(registryDir string, policy RegistryPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	registry, err := openRegistry(registryDir, "")
	if err != nil {
		return err
	}
	lock, err := workspace.AcquireMutation(registry.Dir())
	if err != nil {
		return err
	}
	defer lock.Release()
	entries, err := registry.AllStrict()
	if err != nil {
		return err
	}
	if policy.MaxWorkspaces > 0 && int64(len(entries)) > policy.MaxWorkspaces {
		return fmt.Errorf("stow: existing workspaces exceed registry policy")
	}
	count, size, err := registryCheckpointUsage(registry.Dir())
	if err != nil {
		return err
	}
	if policy.MaxCheckpoints > 0 && count > policy.MaxCheckpoints || policy.MaxCheckpointBytes > 0 && size > policy.MaxCheckpointBytes {
		return fmt.Errorf("stow: existing checkpoints exceed registry policy")
	}
	return registry.WritePolicy(policy)
}

func registryCheckpointUsage(registryDir string) (int64, int64, error) {
	entries, err := os.ReadDir(filepath.Join(registryDir, "checkpoints"))
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var count, size int64
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".checkpoint-") || strings.HasPrefix(entry.Name(), ".import-") {
			continue
		}
		manifest, err := LoadCheckpoint(registryDir, entry.Name())
		if err != nil {
			return 0, 0, fmt.Errorf("stow: invalid retained checkpoint %s: %w", entry.Name(), err)
		}
		bytes := checkpointManifestBytes(manifest)
		if bytes < 0 || size > int64(^uint64(0)>>1)-bytes {
			return 0, 0, fmt.Errorf("stow: retained checkpoint accounting overflow")
		}
		count++
		size += bytes
	}
	return count, size, nil
}

func checkRegistryCheckpointAdmission(registryDir, workspaceID string, bytes int64) error {
	registry, err := openRegistryReadOnly(registryDir, "")
	if err != nil {
		return err
	}
	policy, err := registry.Policy()
	if err != nil {
		return err
	}
	if policy.MaxCheckpoints > 0 || policy.MaxCheckpointBytes > 0 {
		count, size, err := registryCheckpointUsage(registryDir)
		if err != nil {
			return err
		}
		if policy.MaxCheckpoints > 0 && count >= policy.MaxCheckpoints || policy.MaxCheckpointBytes > 0 && (size > policy.MaxCheckpointBytes || bytes > policy.MaxCheckpointBytes-size) {
			return checkpointFailure("capacity_exceeded", "admit", "not_committed", fmt.Errorf("registry checkpoint retention limit exceeded"))
		}
	}
	entry, found, err := registry.Lookup(workspaceID)
	if err != nil {
		return err
	}
	if found {
		return checkCheckpointRetention(registryDir, workspaceID, entry.MaxCheckpointBytes, entry.MaxCheckpoints, bytes)
	}
	return nil
}

// DeleteCheckpoint removes a retained checkpoint and its local replay receipt.
// Local descendants must be deleted first. Request replay is not promised after deletion.
func DeleteCheckpoint(ctx context.Context, registryDir, id string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	dir, err := checkpointDirectory(registryDir, id)
	if err != nil {
		return err
	}
	registryDir = filepath.Dir(filepath.Dir(dir))
	manifest, err := LoadCheckpoint(registryDir, id)
	if err != nil {
		return err
	}
	lock, err := workspace.AcquireMutationCapture(registryDir, manifest.WorkspaceID)
	if err != nil {
		return err
	}
	defer lock.Release()
	if _, err := LoadCheckpoint(registryDir, id); err != nil {
		return err
	}
	if err := checkCheckpointDescendants(registryDir, id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return syncCheckpointPath(filepath.Dir(dir))
}

func checkCheckpointDescendants(registryDir, id string) error {
	entries, err := os.ReadDir(filepath.Join(registryDir, "checkpoints"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".checkpoint-") || strings.HasPrefix(entry.Name(), ".import-") {
			continue
		}
		manifest, err := LoadCheckpoint(registryDir, entry.Name())
		if err != nil {
			return err
		}
		if manifest.ParentID == id {
			return fmt.Errorf("stow: checkpoint has retained descendant %s", manifest.ID)
		}
	}
	return nil
}
