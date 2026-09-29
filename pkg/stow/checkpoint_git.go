package stow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// RestoreCheckpointWithGit rebuilds repository metadata from explicit local sources.
// Captured files remain authoritative, including files deleted since the base commit.
func RestoreCheckpointWithGit(registryDir, id string, options WorkspaceOptions, sources []GitRepositoryInput) (*Workspace, error) {
	manifest, err := LoadCheckpoint(registryDir, id)
	if err != nil {
		return nil, err
	}
	repositories, err := resolveCheckpointGitSources(manifest.Provenance, sources)
	if err != nil {
		return nil, err
	}
	ws, err := RestoreCheckpoint(registryDir, id, options)
	if err != nil {
		return nil, err
	}
	for _, repository := range repositories {
		if err := reconstructCheckpointGit(ws.Dir(), repository); err != nil {
			closeErr := ws.Close()
			return nil, fmt.Errorf("stow: workspace restored at %s but Git reconstruction failed: %w (close: %v)", ws.Dir(), err, closeErr)
		}
	}
	if err := ws.RefreshUsage(context.Background()); err != nil {
		_ = ws.Close()
		return nil, err
	}
	return ws, nil
}

func resolveCheckpointGitSources(provenance *CheckpointProvenance, sources []GitRepositoryInput) ([]PreparedRepository, error) {
	if provenance == nil || len(provenance.Repositories) == 0 {
		return nil, fmt.Errorf("stow: checkpoint has no Git provenance")
	}
	if len(sources) != len(provenance.Repositories) {
		return nil, fmt.Errorf("stow: every checkpoint repository needs one explicit source mapping")
	}
	byDestination := make(map[string]GitRepositoryInput)
	for _, source := range sources {
		if _, duplicate := byDestination[source.Destination]; duplicate {
			return nil, fmt.Errorf("stow: duplicate Git source destination")
		}
		byDestination[source.Destination] = source
	}
	var resolved []PreparedRepository
	for _, repository := range provenance.Repositories {
		source, ok := byDestination[repository.Destination]
		if !ok {
			return nil, fmt.Errorf("stow: missing Git source for %s", repository.Destination)
		}
		source.Ref = repository.Commit
		path, commit, err := resolveGitInput(source)
		if err != nil {
			return nil, err
		}
		if commit != repository.Commit {
			return nil, fmt.Errorf("stow: Git source did not resolve the recorded commit")
		}
		resolved = append(resolved, PreparedRepository{Source: path, Destination: repository.Destination, Commit: commit})
	}
	return resolved, nil
}

func reconstructCheckpointGit(root string, repository PreparedRepository) error {
	destination, err := safeDestination(root, repository.Destination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	if _, err := gitCommand(destination, "init", "--quiet"); err != nil {
		return err
	}
	hooks := filepath.Join(root, ".stow", "disabled-hooks")
	if err := os.MkdirAll(hooks, 0700); err != nil {
		return err
	}
	if _, err := gitCommand(destination, "config", "core.hooksPath", hooks); err != nil {
		return err
	}
	if _, err := gitCommand(destination, "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--depth=1", "--no-recurse-submodules", "--", repository.Source, repository.Commit); err != nil {
		return err
	}
	// Reset rebuilds HEAD and the index without checking base files into the worktree.
	if _, err := gitCommand(destination, "reset", "--mixed", repository.Commit); err != nil {
		return err
	}
	return os.Remove(filepath.Join(destination, ".git", "FETCH_HEAD"))
}
