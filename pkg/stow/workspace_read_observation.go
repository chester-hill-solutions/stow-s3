package stow

import (
	"context"
	"os"
	"sort"
	"time"
)

type WorkspaceDirectoryObservation struct {
	reader        *WorkspaceReader
	path          string
	entries       []WorkspaceReadEntry
	parentInfo    os.FileInfo
	metadataBytes int64
	observedAt    time.Time
}

func (r *WorkspaceReader) ObserveDirectory(ctx context.Context, path string) (*WorkspaceDirectoryObservation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrWorkspaceReaderClosed
	}
	return r.observeDirectoryLocked(ctx, path)
}

func (r *WorkspaceReader) observeDirectoryLocked(ctx context.Context, path string) (*WorkspaceDirectoryObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validWorkspaceReadPath(path, true) || workspaceReadExcluded(path) || !workspaceReadDirectoryAllowed(path, r.readRoots) {
		return nil, ErrWorkspaceReadUnavailable
	}
	directory, before, err := r.openObservationParent(path)
	if err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	defer directory.Close()
	entries, metadataBytes, err := collectWorkspaceReadDirectory(ctx, directory, workspaceReadDirectoryOptions{root: r.root, path: path, readRoots: r.readRoots, maxEntries: maxWorkspaceObservationEntries, maxMetadataBytes: maxWorkspaceObservationBytes})
	if err != nil {
		return nil, err
	}
	if err := r.verifyObservationParent(ctx, path, directory, before); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return &WorkspaceDirectoryObservation{reader: r, path: path, entries: entries, parentInfo: before, metadataBytes: metadataBytes, observedAt: time.Now().UTC()}, nil
}

func (r *WorkspaceReader) openObservationParent(path string) (*os.File, os.FileInfo, error) {
	if err := r.root.Check(); err != nil {
		return nil, nil, ErrWorkspaceReadUnavailable
	}
	directory, err := r.root.OpenDirectory(path)
	if err != nil {
		return nil, nil, ErrWorkspaceReadUnavailable
	}
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		_ = directory.Close()
		return nil, nil, ErrWorkspaceReadUnavailable
	}
	return directory, info, nil
}

func (r *WorkspaceReader) verifyObservationParent(ctx context.Context, path string, directory *os.File, before os.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	after, err := directory.Stat()
	if err != nil || workspaceReadDirectoryChanged(before, after) {
		return ErrWorkspaceReadChanged
	}
	current, err := r.root.OpenDirectory(path)
	if err != nil {
		return ErrWorkspaceReadChanged
	}
	currentInfo, statErr := current.Stat()
	closeErr := current.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(before, currentInfo) {
		return ErrWorkspaceReadChanged
	}
	if err := r.root.Check(); err != nil {
		return ErrWorkspaceReadChanged
	}
	return nil
}

func workspaceReadDirectoryChanged(before, after os.FileInfo) bool {
	return !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime())
}

func (o *WorkspaceDirectoryObservation) Page(ctx context.Context, offset, limit int) ([]WorkspaceReadEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r := o.reader
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrWorkspaceReaderClosed
	}
	if err := validateObservationPage(ctx, r, o, offset, limit); err != nil {
		return nil, err
	}
	if err := verifyObservationPageParent(r, o); err != nil {
		return nil, err
	}
	return copyObservationPage(ctx, o.entries, offset, limit)
}

func validateObservationPage(ctx context.Context, reader *WorkspaceReader, observation *WorkspaceDirectoryObservation, offset, limit int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if offset < 0 || offset > len(observation.entries) || limit < 1 || limit > maxWorkspaceReadEntries {
		return ErrWorkspaceReadUnavailable
	}
	if !workspaceReadDirectoryAllowed(observation.path, reader.readRoots) || workspaceReadExcluded(observation.path) {
		return ErrWorkspaceReadUnavailable
	}
	return nil
}

func verifyObservationPageParent(reader *WorkspaceReader, observation *WorkspaceDirectoryObservation) error {
	if err := reader.root.Check(); err != nil {
		return ErrWorkspaceReadChanged
	}
	directory, err := reader.root.OpenDirectory(observation.path)
	if err != nil {
		return ErrWorkspaceReadChanged
	}
	info, statErr := directory.Stat()
	closeErr := directory.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(observation.parentInfo, info) {
		return ErrWorkspaceReadChanged
	}
	return nil
}

func copyObservationPage(ctx context.Context, entries []WorkspaceReadEntry, offset, limit int) ([]WorkspaceReadEntry, error) {
	end := min(offset+limit, len(entries))
	page := make([]WorkspaceReadEntry, 0, end-offset)
	for _, entry := range entries[offset:end] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page = append(page, entry)
	}
	return page, nil
}

func (o *WorkspaceDirectoryObservation) Len() int { return len(o.entries) }

func (o *WorkspaceDirectoryObservation) MetadataBytes() int64 { return o.metadataBytes }

func (o *WorkspaceDirectoryObservation) ObservedAt() time.Time { return o.observedAt }

func (o *WorkspaceDirectoryObservation) Path() string { return o.path }
