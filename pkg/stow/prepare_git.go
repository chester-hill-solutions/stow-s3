package stow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cloneGitRepositories fetches one explicit commit into a fresh shallow
// repository. Its history is independent and its lifetime matches the owned
// workspace; the source repository gets no worktree registration.
func cloneGitRepositories(root string, inputs []GitRepositoryInput, includeSensitive bool) (int64, int64, []PreparedRepository, error) {
	if len(inputs) == 0 {
		return 0, 0, nil, nil
	}
	hooksDirectory := filepath.Join(root, ".stow", "disabled-hooks")
	if err := os.MkdirAll(hooksDirectory, 0o700); err != nil {
		return 0, 0, nil, fmt.Errorf("stow: prepare an empty Git hooks directory: %w", err)
	}
	return prepareGitInputs(root, hooksDirectory, inputs, includeSensitive)
}

func prepareGitInputs(root, hooksDirectory string, inputs []GitRepositoryInput, includeSensitive bool) (int64, int64, []PreparedRepository, error) {
	var totalBytes, totalObjects int64
	prepared := make([]PreparedRepository, 0, len(inputs))
	for _, input := range inputs {
		repository, bytes, objects, err := prepareGitInput(root, hooksDirectory, input, includeSensitive)
		if err != nil {
			return 0, 0, nil, err
		}
		if bytes > int64(^uint64(0)>>1)-totalBytes || objects > int64(^uint64(0)>>1)-totalObjects {
			return 0, 0, nil, fmt.Errorf("stow: Git seed totals exceed supported limits")
		}
		totalBytes += bytes
		totalObjects += objects
		prepared = append(prepared, repository)
	}
	return totalBytes, totalObjects, prepared, nil
}

func prepareGitInput(root, hooksDirectory string, input GitRepositoryInput, includeSensitive bool) (PreparedRepository, int64, int64, error) {
	source, commit, err := resolveGitInput(input)
	if err != nil {
		return PreparedRepository{}, 0, 0, err
	}
	destination, err := safeDestination(root, input.Destination)
	if err != nil {
		return PreparedRepository{}, 0, 0, fmt.Errorf("stow: invalid Git destination %q: %w", input.Destination, err)
	}
	if err := initializeTaskRepository(destination); err != nil {
		return PreparedRepository{}, 0, 0, err
	}
	if err := fetchAndCheckout(destination, hooksDirectory, source, commit, input); err != nil {
		return PreparedRepository{}, 0, 0, err
	}
	bytes, objects, err := measureGitSeed(destination, includeSensitive)
	if err != nil {
		return PreparedRepository{}, 0, 0, err
	}
	return PreparedRepository{Source: source, Destination: filepath.ToSlash(input.Destination), Ref: input.Ref, Commit: commit}, bytes, objects, nil
}

func resolveGitInput(input GitRepositoryInput) (string, string, error) {
	if strings.TrimSpace(input.Source) == "" || strings.TrimSpace(input.Ref) == "" {
		return "", "", fmt.Errorf("stow: Git repository source and explicit ref are required")
	}
	source, err := gitOutput(input.Source, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", fmt.Errorf("stow: inspect Git source %q: %w", input.Source, err)
	}
	source = strings.TrimSpace(source)
	commit, err := gitOutput(source, "rev-parse", "--verify", "--end-of-options", input.Ref+"^{commit}")
	if err != nil {
		return "", "", fmt.Errorf("stow: resolve Git ref %q in %q: %w", input.Ref, input.Source, err)
	}
	return source, strings.TrimSpace(commit), nil
}

func initializeTaskRepository(destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("stow: create Git destination parent: %w", err)
	}
	if err := os.Mkdir(destination, 0o755); err != nil {
		return fmt.Errorf("stow: reserve Git destination %q: %w", destination, err)
	}
	if _, err := gitCommand(destination, "init", "--quiet"); err != nil {
		return fmt.Errorf("stow: initialize task repository: %w", err)
	}
	return nil
}

func fetchAndCheckout(destination, hooksDirectory, source, commit string, input GitRepositoryInput) error {
	if _, err := gitCommand(destination, "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--depth=1", "--no-recurse-submodules", "--", source, commit); err != nil {
		return fmt.Errorf("stow: fetch Git ref %q from %q: %w", input.Ref, input.Source, err)
	}
	if _, err := gitCommand(destination, "-c", "core.hooksPath="+hooksDirectory, "checkout", "--detach", "--no-recurse-submodules", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("stow: check out Git ref %q: %w", input.Ref, err)
	}
	if err := os.Remove(filepath.Join(destination, ".git", "FETCH_HEAD")); err != nil {
		return fmt.Errorf("stow: remove temporary Git fetch metadata: %w", err)
	}
	actual, err := gitOutput(destination, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(actual) != commit {
		return fmt.Errorf("stow: Git checkout did not resolve to requested commit %s", commit)
	}
	return nil
}

func measureGitSeed(root string, includeSensitive bool) (int64, int64, error) {
	var totalBytes, totalObjects int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !includeSensitive && sensitiveSeedPath(rel) {
			return fmt.Errorf("stow: sensitive-looking Git input %s is excluded by default (set include_sensitive_inputs explicitly to include it)", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("stow: Git input supports regular files only: %s", rel)
		}
		if info.Size() > int64(^uint64(0)>>1)-totalBytes || totalObjects == int64(^uint64(0)>>1) {
			return fmt.Errorf("stow: Git seed totals exceed supported limits")
		}
		totalBytes += info.Size()
		totalObjects++
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("stow: inspect Git seed: %w", err)
	}
	return totalBytes, totalObjects, nil
}

func gitOutput(directory string, args ...string) (string, error) {
	output, err := gitCommand(directory, args...)
	return string(output), err
}

func gitCommand(directory string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", directory}, args...)
	command := exec.CommandContext(context.Background(), "git", commandArgs...)
	command.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_LFS_SKIP_SMUDGE=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
