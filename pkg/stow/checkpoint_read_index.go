package stow

import (
	"path"
	"sort"
	"strings"
)

func indexCheckpointReadFiles(files []CheckpointFile, configuredRoots []string) (map[string]CheckpointFile, map[string][]WorkspaceReadEntry, []string, error) {
	if len(files) > checkpointReadMaxFiles {
		return nil, nil, nil, ErrCheckpointReadTooLarge
	}
	index := newCheckpointReadIndex(len(files))
	for _, file := range files {
		if err := index.add(file); err != nil {
			return nil, nil, nil, err
		}
	}
	roots, err := normalizeCheckpointRoots(index.directories, configuredRoots)
	if err != nil {
		return nil, nil, nil, err
	}
	directories := visibleCheckpointDirectories(index.directories, roots)
	return index.files, directories, roots, nil
}

type checkpointReadIndex struct {
	files       map[string]CheckpointFile
	directories map[string]map[string]WorkspaceReadEntry
	kinds       map[string]string
	metadata    int64
}

func newCheckpointReadIndex(fileCount int) *checkpointReadIndex {
	return &checkpointReadIndex{
		files:       make(map[string]CheckpointFile, fileCount),
		directories: map[string]map[string]WorkspaceReadEntry{".": {}},
		kinds:       make(map[string]string, fileCount*2), metadata: 256,
	}
}

func (index *checkpointReadIndex) add(file CheckpointFile) error {
	if !validWorkspaceReadPath(file.Path, false) || !validCheckpointPath(file.Path) {
		return ErrCheckpointReadUnavailable
	}
	if workspaceReadExcluded(file.Path) {
		return nil
	}
	if _, exists := index.files[file.Path]; exists || index.kinds[file.Path] == "directory" {
		return ErrCheckpointReadUnavailable
	}
	index.files[file.Path] = file
	index.kinds[file.Path] = "file"
	index.metadata += int64(256 + len(path.Base(file.Path)) + len(file.Path) + len(file.SHA256))
	if index.metadata > checkpointReadMaxMetadata {
		return ErrCheckpointReadTooLarge
	}
	if err := addCheckpointDirectories(index.directories, index.kinds, file.Path, &index.metadata); err != nil {
		return err
	}
	parent := path.Dir(file.Path)
	entry := WorkspaceReadEntry{Name: path.Base(file.Path), Path: file.Path, Kind: "file", Size: file.Size}
	index.directories[parent][entry.Name] = entry
	return nil
}

func visibleCheckpointDirectories(raw map[string]map[string]WorkspaceReadEntry, roots []string) map[string][]WorkspaceReadEntry {
	directories := make(map[string][]WorkspaceReadEntry, len(raw))
	for directory, children := range raw {
		if !workspaceReadDirectoryAllowed(directory, roots) || workspaceReadExcluded(directory) {
			continue
		}
		entries := make([]WorkspaceReadEntry, 0, len(children))
		for _, entry := range children {
			if !workspaceReadEntryAllowed(entry.Path, roots) ||
				(workspaceReadDirectoryIsAncestor(entry.Path, roots) && entry.Kind != "directory") {
				continue
			}
			entries = append(entries, entry)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		directories[directory] = entries
	}
	return directories
}

func addCheckpointDirectories(directories map[string]map[string]WorkspaceReadEntry, kinds map[string]string, filePath string, charge *int64) error {
	parts := strings.Split(filePath, "/")
	parent := "."
	for _, name := range parts[:len(parts)-1] {
		child := name
		if parent != "." {
			child = parent + "/" + name
		}
		if kinds[child] == "file" {
			return ErrCheckpointReadUnavailable
		}
		if kinds[child] == "" {
			kinds[child] = "directory"
			*charge += int64(256 + len(name) + len(child))
			if *charge > checkpointReadMaxMetadata {
				return ErrCheckpointReadTooLarge
			}
			directories[child] = map[string]WorkspaceReadEntry{}
			directories[parent][name] = WorkspaceReadEntry{Name: name, Path: child, Kind: "directory"}
		}
		parent = child
	}
	return nil
}

func normalizeCheckpointRoots(directories map[string]map[string]WorkspaceReadEntry, configured []string) ([]string, error) {
	if configured == nil {
		configured = []string{"."}
	}
	if len(configured) == 0 {
		return nil, ErrCheckpointReadUnavailable
	}
	seen := make(map[string]struct{}, min(len(configured), maxWorkspaceReadRoots))
	roots := make([]string, 0, min(len(configured), maxWorkspaceReadRoots))
	for _, root := range configured {
		if !validWorkspaceReadPath(root, true) || workspaceReadExcluded(root) ||
			(root != "." && !validCheckpointPath(root)) {
			return nil, ErrCheckpointReadUnavailable
		}
		if _, found := seen[root]; found {
			continue
		}
		if len(roots) == maxWorkspaceReadRoots || directories[root] == nil {
			return nil, ErrCheckpointReadUnavailable
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots, nil
}
