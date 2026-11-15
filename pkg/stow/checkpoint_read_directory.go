package stow

import (
	"context"
	"strings"
)

func (r *CheckpointReader) ReadDirectory(ctx context.Context, path string, offset, limit int) (CheckpointReadDirectory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	opCtx, cancel, err := r.operationContext(ctx)
	if err != nil {
		return CheckpointReadDirectory{}, err
	}
	defer cancel()
	if !validWorkspaceReadPath(path, true) || workspaceReadExcluded(path) || !workspaceReadDirectoryAllowed(path, r.readRoots) || limit < 1 || limit > maxWorkspaceReadEntries {
		return CheckpointReadDirectory{}, ErrCheckpointReadUnavailable
	}
	if err := r.verifyManifest(opCtx); err != nil {
		return CheckpointReadDirectory{}, r.operationError(opCtx, err)
	}
	entries, found := r.directories[path]
	if !found || offset < 0 || offset > len(entries) {
		return CheckpointReadDirectory{}, ErrCheckpointReadUnavailable
	}
	if err := opCtx.Err(); err != nil {
		return CheckpointReadDirectory{}, r.operationError(opCtx, err)
	}
	end := min(offset+limit, len(entries))
	page := append([]WorkspaceReadEntry(nil), entries[offset:end]...)
	if err := opCtx.Err(); err != nil {
		return CheckpointReadDirectory{}, r.operationError(opCtx, err)
	}
	return CheckpointReadDirectory{Entries: page, Offset: offset, Total: len(entries)}, nil
}

func (r *CheckpointReader) verifyManifest(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := readCheckpointManifestBytes(ctx, r.root)
	if err != nil {
		return ErrCheckpointReadUnavailable
	}
	digest := checkpointManifestDigest(data)
	if !strings.EqualFold(digest, r.reference.ManifestSHA256) {
		return ErrCheckpointReadUnavailable
	}
	return nil
}
