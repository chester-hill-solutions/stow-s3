package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MutationLock struct {
	registry *CaptureLock
	capture  *CaptureLock
}

func AcquireMutation(registryDir string) (*CaptureLock, error) {
	return AcquireCapture(filepath.Join(registryDir, ".stow"), "registry_mutation")
}

// AcquireMutationCapture fixes mutation ordering: registry, then workspace.
func AcquireMutationCapture(registryDir, id string) (*MutationLock, error) {
	registry, err := AcquireMutation(registryDir)
	if err != nil {
		return nil, err
	}
	capture, err := AcquireCapture(registryDir, id)
	if err != nil {
		_ = registry.Release()
		return nil, err
	}
	return &MutationLock{registry: registry, capture: capture}, nil
}

func (l *MutationLock) Release() error {
	first := l.capture.Release()
	second := l.registry.Release()
	if first != nil {
		return first
	}
	return second
}

type RegistryPolicy struct {
	Version            int   `json:"version"`
	MaxWorkspaces      int64 `json:"max_workspaces"`
	MaxCheckpoints     int64 `json:"max_checkpoints"`
	MaxCheckpointBytes int64 `json:"max_checkpoint_bytes"`
}

func (p RegistryPolicy) Validate() error {
	if p.Version != 1 || p.MaxWorkspaces < 0 || p.MaxCheckpoints < 0 || p.MaxCheckpointBytes < 0 {
		return fmt.Errorf("workspace: invalid registry policy version or limits")
	}
	return nil
}

func (r *Registry) Policy() (RegistryPolicy, error) {
	policy := RegistryPolicy{Version: 1}
	data, err := os.ReadFile(filepath.Join(r.dir, ".stow", "policy.json"))
	if os.IsNotExist(err) {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		return policy, fmt.Errorf("workspace: decode registry policy: %w", err)
	}
	return policy, policy.Validate()
}

// WritePolicy requires the caller's registry mutation gate and usage admission.
func (r *Registry) WritePolicy(policy RegistryPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(r.dir, ".stow", "policy.json"), data); err != nil {
		return err
	}
	return syncRegistryDirectory(r.dir)
}

func (r *Registry) AllStrict() ([]Entry, error) {
	names, err := os.ReadDir(r.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, name := range names {
		if name.IsDir() || filepath.Ext(name.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(name.Name(), ".json")
		entry, found, err := r.Lookup(id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("workspace: registry changed during accounting")
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (r *Registry) RegisterWithLimit(entry Entry, maxWorkspaces int64) error {
	lock, err := acquireCapture(filepath.Join(r.dir, ".stow"), "registry_mutation", waitLock)
	if err != nil {
		return err
	}
	defer lock.Release()
	policy, err := r.Policy()
	if err != nil {
		return err
	}
	if maxWorkspaces < 0 {
		return fmt.Errorf("workspace: negative workspace cap")
	}
	if policy.MaxWorkspaces > 0 && (maxWorkspaces == 0 || policy.MaxWorkspaces < maxWorkspaces) {
		maxWorkspaces = policy.MaxWorkspaces
	}
	_, found, err := r.Lookup(entry.ID)
	if err != nil {
		return err
	}
	if !found && maxWorkspaces > 0 {
		entries, err := r.AllStrict()
		if err != nil {
			return err
		}
		if int64(len(entries)) >= maxWorkspaces {
			return fmt.Errorf("workspace: workspace count limit reached (%d of %d)", len(entries), maxWorkspaces)
		}
	}
	return r.register(entry)
}

func syncRegistryDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
