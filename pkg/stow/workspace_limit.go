package stow

import (
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// checkWorkspaceRetention refuses a prepare that would take a registry past the
// caller's declared bound, and reports how many workspaces it found.
//
// Shaped like checkCheckpointRetention: zero is unlimited, and a cap refuses rather
// than removing anything — collecting the oldest idle workspace destroys a task
// nobody said was finished, and an adopted entry is somebody's project.
//
// The count is of the team partition, because that is the unit the other verbs sweep
// and report. A bound on the root would let one team's workspaces consume another's
// quota on a shared runner, which is the partitioning's whole reason to exist.
func checkWorkspaceRetention(registryDir, team string, maxWorkspaces int64) (int64, error) {
	if maxWorkspaces <= 0 {
		return 0, nil
	}
	resolved, err := ResolveRegistryDir(registryDir, team)
	if err != nil {
		return 0, err
	}
	entries, err := countRegistryEntries(resolved)
	if err != nil {
		return 0, err
	}
	if entries >= maxWorkspaces {
		return entries, fmt.Errorf(
			"stow: workspace count limit reached (%d of %d); "+
				"run `stow-s3 workspace prune` to forget entries whose directory is gone, "+
				"`stow-s3 workspace collect` to reclaim finished workspaces, "+
				"or raise max_workspaces",
			entries, maxWorkspaces)
	}
	return entries, nil
}

// countRegistryEntries reports how many workspaces a registry records, not how many
// directories exist. A record whose directory is gone is what makes a registry large
// — 1959 of the 1961 entries on the machine this was written against — and it is the
// one prune clears without touching data, so a directory count would report that
// registry as nearly empty.
func countRegistryEntries(registryDir string) (int64, error) {
	if registryDir == "" {
		return 0, nil
	}
	registry, err := workspace.OpenRegistryReadOnly(registryDir)
	if err != nil {
		return 0, fmt.Errorf("stow: count registry entries: %w", err)
	}
	entries, err := registry.AllStrict()
	if err != nil {
		return 0, fmt.Errorf("stow: count registry entries: %w", err)
	}
	return int64(len(entries)), nil
}
