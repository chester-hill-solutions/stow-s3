package stow

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

func (r *WorkspaceReader) readWorkspaceDirectory(ctx context.Context, path string) ([]WorkspaceReadEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrWorkspaceReaderClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validWorkspaceReadPath(path, true) || workspaceReadExcluded(path) || !workspaceReadDirectoryAllowed(path, r.readRoots) {
		return nil, ErrWorkspaceReadUnavailable
	}
	if err := r.root.Check(); err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	directory, err := r.openWorkspaceDirectory(path)
	if err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	defer directory.Close()
	entries, _, err := collectWorkspaceReadDirectory(ctx, directory, workspaceReadDirectoryOptions{root: r.root, path: path, readRoots: r.readRoots, maxEntries: r.maxEntries})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.root.Check(); err != nil {
		return nil, ErrWorkspaceReadChanged
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

type workspaceReadDirectoryOptions struct {
	root             *rooted.Root
	path             string
	readRoots        []string
	maxEntries       int
	maxMetadataBytes int64
}

func collectWorkspaceReadDirectory(ctx context.Context, directory *os.File, options workspaceReadDirectoryOptions) ([]WorkspaceReadEntry, int64, error) {
	entries := make([]WorkspaceReadEntry, 0, min(options.maxEntries, 32))
	scan := workspaceReadDirectoryScan{root: options.root, path: options.path, readRoots: options.readRoots, maxEntries: options.maxEntries, maxMetadataBytes: options.maxMetadataBytes, metadataBytes: 256}
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		batch, readErr := directory.ReadDir(128)
		if readErr != nil && readErr != io.EOF {
			return nil, 0, ErrWorkspaceReadUnavailable
		}
		if len(batch) == 0 && readErr == io.EOF {
			break
		}
		var batchErr error
		entries, batchErr = scan.append(ctx, entries, batch)
		if batchErr != nil {
			return nil, 0, batchErr
		}
		if readErr == io.EOF {
			break
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for index := 1; index < len(entries); index++ {
		if entries[index-1].Path == entries[index].Path {
			return nil, 0, ErrWorkspaceReadChanged
		}
	}
	return entries, scan.metadataBytes, nil
}

type workspaceReadDirectoryScan struct {
	root             *rooted.Root
	path             string
	readRoots        []string
	maxEntries       int
	scanned          int
	maxMetadataBytes int64
	metadataBytes    int64
}

func (s *workspaceReadDirectoryScan) append(ctx context.Context, entries []WorkspaceReadEntry, batch []os.DirEntry) ([]WorkspaceReadEntry, error) {
	for _, entry := range batch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.scanned++
		if s.scanned > maxWorkspaceReadScannedEntries {
			return nil, ErrWorkspaceReadTooLarge
		}
		var err error
		entries, err = s.appendEntry(entries, entry.Name())
		if err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (s *workspaceReadDirectoryScan) appendEntry(entries []WorkspaceReadEntry, name string) ([]WorkspaceReadEntry, error) {
	childPath := workspaceReadChildPath(s.path, name)
	if workspaceReadExcluded(childPath) || !workspaceReadEntryAllowed(childPath, s.readRoots) {
		return entries, nil
	}
	if !utf8.ValidString(name) || !validWorkspaceReadSegment(name) || len(childPath) > maxWorkspaceReadPathBytes {
		return nil, ErrWorkspaceReadUnavailable
	}
	child, visible, err := inspectWorkspaceReadChild(s.root, childPath, name)
	if err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	if !visible || (workspaceReadDirectoryIsAncestor(childPath, s.readRoots) && child.Kind != "directory") {
		return entries, nil
	}
	charge := int64(256 + len(child.Name) + len(child.Path))
	if s.maxMetadataBytes > 0 && s.metadataBytes+charge > s.maxMetadataBytes {
		return nil, ErrWorkspaceReadTooLarge
	}
	s.metadataBytes += charge
	entries = append(entries, child)
	if len(entries) > s.maxEntries {
		return nil, ErrWorkspaceReadTooLarge
	}
	return entries, nil
}

func workspaceReadChildPath(parent, name string) string {
	if parent == "." {
		return name
	}
	return parent + "/" + name
}

func (r *WorkspaceReader) openWorkspaceDirectory(path string) (*os.File, error) {
	return r.root.OpenDirectory(path)
}

func inspectWorkspaceReadChild(root *rooted.Root, path, name string) (WorkspaceReadEntry, bool, error) {
	if file, err := root.OpenDirectory(path); err == nil {
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil || closeErr != nil || !info.IsDir() {
			return WorkspaceReadEntry{}, false, ErrWorkspaceReadChanged
		}
		return WorkspaceReadEntry{Name: name, Path: path, Kind: "directory"}, true, nil
	} else if !errors.Is(err, rooted.ErrNotDirectory) && !errors.Is(err, rooted.ErrSymlink) && !errors.Is(err, os.ErrNotExist) {
		return WorkspaceReadEntry{}, false, err
	}
	file, err := root.OpenRegularFile(path)
	if err != nil {
		if errors.Is(err, rooted.ErrNotRegular) || errors.Is(err, rooted.ErrSymlink) || errors.Is(err, os.ErrNotExist) {
			return WorkspaceReadEntry{}, false, nil
		}
		return WorkspaceReadEntry{}, false, err
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || !info.Mode().IsRegular() {
		return WorkspaceReadEntry{}, false, ErrWorkspaceReadChanged
	}
	return WorkspaceReadEntry{Name: name, Path: path, Kind: "file", Size: info.Size()}, true, nil
}
