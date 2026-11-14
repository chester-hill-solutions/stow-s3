package stow

import (
	"sort"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

const maxWorkspaceReadRoots = 16

func normalizeWorkspaceReadRoots(root *rooted.Root, configured []string) ([]string, error) {
	if configured == nil {
		configured = []string{"."}
	}
	if len(configured) == 0 {
		return nil, ErrWorkspaceReadUnavailable
	}
	roots := make([]string, 0, min(len(configured), maxWorkspaceReadRoots))
	seen := make(map[string]struct{}, min(len(configured), maxWorkspaceReadRoots))
	for _, path := range configured {
		if !validWorkspaceReadPath(path, true) || workspaceReadExcluded(path) {
			return nil, ErrWorkspaceReadUnavailable
		}
		if _, ok := seen[path]; ok {
			continue
		}
		if len(roots) == maxWorkspaceReadRoots {
			return nil, ErrWorkspaceReadUnavailable
		}
		if err := verifyWorkspaceReadRoot(root, path); err != nil {
			return nil, ErrWorkspaceReadUnavailable
		}
		seen[path] = struct{}{}
		roots = append(roots, path)
	}
	sort.Strings(roots)
	return roots, nil
}

func verifyWorkspaceReadRoot(root *rooted.Root, path string) error {
	if err := root.Check(); err != nil {
		return err
	}
	directory, err := root.OpenDirectory(path)
	if err != nil {
		return err
	}
	info, statErr := directory.Stat()
	closeErr := directory.Close()
	if statErr != nil || closeErr != nil || !info.IsDir() {
		return ErrWorkspaceReadChanged
	}
	return root.Check()
}

func workspaceReadWithin(root, candidate string) bool {
	if root == "." {
		return true
	}
	return candidate == root || strings.HasPrefix(candidate, root+"/")
}

func workspaceReadDirectoryAllowed(path string, roots []string) bool {
	return workspaceReadEntryAllowed(path, roots)
}

func workspaceReadFileAllowed(path string, roots []string) bool {
	for _, root := range roots {
		if workspaceReadWithin(root, path) {
			return true
		}
	}
	return false
}

func workspaceReadEntryAllowed(path string, roots []string) bool {
	for _, root := range roots {
		if workspaceReadWithin(root, path) || workspaceReadWithin(path, root) {
			return true
		}
	}
	return false
}

func workspaceReadDirectoryIsAncestor(path string, roots []string) bool {
	for _, root := range roots {
		if workspaceReadWithin(path, root) && path != root {
			return true
		}
	}
	return false
}
