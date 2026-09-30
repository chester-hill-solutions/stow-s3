package stow

import (
	"context"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// DestroyRegisteredWorkspace is an explicit host-administrative operation over
// a trusted registry. It does not use or widen a restricted Workspace handle.
// Hosts must not expose it to actors solely because they can read a workspace.
// Live, adopted, protected and identity-mismatched directories are refused.
func DestroyRegisteredWorkspace(ctx context.Context, registryDir, id string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	registry, err := openRegistry(registryDir, "")
	if err != nil {
		return err
	}
	return registry.RemoveWorkspace(id, func() error {
		entry, err := lookupWorkspaceEntry(registry, id)
		if err != nil {
			return err
		}
		return destroyRegisteredEntry(ctx, entry)
	})
}

func destroyRegisteredEntry(ctx context.Context, entry workspace.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session, err := workspace.AcquireSession(entry.Dir)
	if err != nil {
		return err
	}
	defer session.Release()
	store, err := workspace.New(workspace.Options{Root: entry.Dir, Bucket: entry.Bucket})
	if err != nil {
		return err
	}
	defer store.Close()
	if store.ID() != entry.ID {
		return fmt.Errorf("stow: workspace %s does not match the workspace registered at %s", entry.ID, entry.Dir)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.Destroy()
}
