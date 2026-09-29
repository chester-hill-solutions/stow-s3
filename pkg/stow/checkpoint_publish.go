package stow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"
)

func publishCheckpoint(tempDir, checkpointRoot, id string, manifest CheckpointManifest) error {
	return publishCheckpointContext(context.Background(), tempDir, checkpointRoot, id, manifest)
}

type checkpointPublisher struct {
	syncPath func(string) error
	rename   func(string, string) error
}

func publishCheckpointContext(ctx context.Context, tempDir, checkpointRoot, id string, manifest CheckpointManifest) error {
	if manifest.ID != id {
		return fmt.Errorf("stow: checkpoint publication identity mismatch")
	}
	publisher := checkpointPublisher{syncPath: syncCheckpointPath, rename: os.Rename}
	return publisher.publish(ctx, tempDir, checkpointRoot, manifest)
}

func (p checkpointPublisher) publish(ctx context.Context, tempDir, checkpointRoot string, manifest CheckpointManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("stow: encode checkpoint: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "manifest.json"), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("stow: write checkpoint manifest: %w", err)
	}
	if err := p.syncTree(ctx, tempDir); err != nil {
		return err
	}
	if err := p.syncPath(filepath.Dir(checkpointRoot)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Rename is the visibility boundary. After it, failure means an unknown durable outcome.
	if err := p.rename(tempDir, filepath.Join(checkpointRoot, manifest.ID)); err != nil {
		return fmt.Errorf("stow: publish checkpoint: %w", err)
	}
	if err := p.syncPath(checkpointRoot); err != nil {
		return checkpointFailure("outcome_unknown", "commit", "unknown", err)
	}
	return nil
}

func syncCheckpointTree(ctx context.Context, root string) error {
	return (checkpointPublisher{syncPath: syncCheckpointPath}).syncTree(ctx, root)
}

func (p checkpointPublisher) syncTree(ctx context.Context, root string) error {
	var dirs []string
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("stow: invalid checkpoint payload %q", path)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}
	if err := p.syncFiles(ctx, files); err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.syncPath(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}

// Independent files may flush together. Every flush finishes before directory
// synchronization or publication; a failed flush still prevents acknowledgement.
// Bounding concurrency avoids one descriptor/goroutine per captured file.
func (p checkpointPublisher) syncFiles(ctx context.Context, files []string) error {
	group, pending := errgroup.WithContext(ctx)
	group.SetLimit(16)
	for _, path := range files {
		if pending.Err() != nil {
			break
		}
		group.Go(func() error {
			if err := pending.Err(); err != nil {
				return err
			}
			return p.syncPath(path)
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}

func syncCheckpointPath(path string) error {
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
