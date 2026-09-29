package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func previewCheckpointCommand(args []string) error {
	flags := flag.NewFlagSet("workspace preview", flag.ContinueOnError)
	archivePath := flags.String("archive", "", "Checkpoint archive to inspect")
	maxBytes := flags.Int64("max-bytes", 0, "Uncompressed byte cap (0 uses the 1 GiB default)")
	maxFiles := flags.Int64("max-files", 0, "File count cap (0 uses the 100000-file default)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return errors.New("workspace preview requires --archive")
	}
	if *maxBytes < 0 || *maxFiles < 0 {
		return errors.New("workspace preview limits must not be negative")
	}
	file, err := os.Open(*archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	preview, err := stow.PreviewCheckpointArchive(context.Background(), file, stow.CheckpointArchiveOptions{
		MaxBytes: *maxBytes, MaxFiles: *maxFiles,
	})
	if err != nil {
		return err
	}
	return writeWorkspaceJSON(preview)
}

func exportCheckpointCommand(args []string) error {
	flags := flag.NewFlagSet("workspace export", flag.ContinueOnError)
	id := flags.String("checkpoint-id", "", "Checkpoint ID to export")
	chosen := registryFlag(flags)
	output := flags.String("output", "", "New archive file path")
	maxBytes := flags.Int64("max-bytes", 0, "Uncompressed byte cap (0 uses the 1 GiB default)")
	maxFiles := flags.Int64("max-files", 0, "File count cap (0 uses the 100000-file default)")
	includeSensitive := flags.Bool("include-sensitive", false, "Allow sensitive-looking paths in the archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *id == "" || *output == "" {
		return errors.New("workspace export requires --checkpoint-id and --output")
	}
	if *maxBytes < 0 || *maxFiles < 0 {
		return errors.New("workspace export limits must not be negative")
	}
	selection := chosen()
	registry, err := selection.resolve("")
	if err != nil {
		return err
	}
	if err := exportCheckpointFile(registry, *id, *output, stow.CheckpointArchiveOptions{
		MaxBytes: *maxBytes, MaxFiles: *maxFiles, IncludeSensitiveFiles: *includeSensitive,
	}); err != nil {
		return err
	}
	return writeWorkspaceJSON(struct {
		Version      int    `json:"version"`
		CheckpointID string `json:"checkpoint_id"`
		Archive      string `json:"archive"`
	}{Version: 1, CheckpointID: *id, Archive: *output})
}

func exportCheckpointFile(registryDir, id, output string, options stow.CheckpointArchiveOptions) error {
	_, err := publishNewFile(output, "archive", func(file *os.File) error {
		return stow.ExportCheckpoint(context.Background(), registryDir, id, file, options)
	})
	return err
}

// publishNewFile writes through a temporary file in the destination's own
// directory and links it into place.
//
// Both properties matter and neither comes from the write. A reader never sees a
// partial file, because the name appears atomically. An existing destination is
// refused rather than replaced, because every artifact this product publishes —
// an archive, a delta document, a handoff reference — is something a caller may
// already be holding a path to, and overwriting it would swap the bytes out from
// under that reference.
func publishNewFile(destination, label string, write func(*os.File) error) (string, error) {
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return "", fmt.Errorf("%s destination already exists: %s", label, absolute)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(absolute), ".stow-publish-*")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := write(temp); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tempName, absolute); err != nil {
		return "", fmt.Errorf("publish %s: %w", label, err)
	}
	return absolute, nil
}

func importCheckpointCommand(args []string) error {
	flags := flag.NewFlagSet("workspace import", flag.ContinueOnError)
	archivePath := flags.String("archive", "", "Checkpoint archive file")
	chosen := registryFlag(flags)
	maxBytes := flags.Int64("max-bytes", 0, "Uncompressed byte cap (0 uses the 1 GiB default)")
	maxFiles := flags.Int64("max-files", 0, "File count cap (0 uses the 100000-file default)")
	includeSensitive := flags.Bool("include-sensitive", false, "Allow sensitive-looking paths in the archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return errors.New("workspace import requires --archive")
	}
	if *maxBytes < 0 || *maxFiles < 0 {
		return errors.New("workspace import limits must not be negative")
	}
	selection := chosen()
	registry, err := selection.resolve("")
	if err != nil {
		return err
	}
	file, err := os.Open(*archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	checkpoint, err := stow.ImportCheckpoint(context.Background(), registry, file, stow.CheckpointArchiveOptions{
		MaxBytes: *maxBytes, MaxFiles: *maxFiles, IncludeSensitiveFiles: *includeSensitive,
	})
	if err != nil {
		return err
	}
	return writeWorkspaceJSON(checkpointResult{
		Version: 1, ID: checkpoint.ID, WorkspaceID: checkpoint.WorkspaceID,
		ParentID: checkpoint.ParentID, Created: checkpoint.Created.Format(time.RFC3339Nano),
		Files: checkpoint.Files, Bytes: checkpoint.Bytes, Excluded: checkpoint.Excluded,
	})
}

// checkpointWorkspaceCommand captures a checkpoint of a workspace that another
// process may be using right now.
//
// It resolves the registry and the workspace's own recorded limits, and never
// claims the session: an agent holding the workspace is not asked to stop so the
// work so far can be captured. The capture is the one Workspace.CreateCheckpoint
// runs, so the exclusions, the limits, and the refusal of a tree that changed
// mid-capture are identical either way.
func checkpointWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace checkpoint", flag.ContinueOnError)
	id := flags.String("id", "", "Workspace ID to checkpoint")
	chosen := registryFlag(flags)
	parentID := flags.String("parent", "", "Parent checkpoint ID")
	maxBytes := flags.Int64("max-bytes", 0, "Checkpoint byte cap (0 uses no separate cap)")
	maxFiles := flags.Int64("max-files", 0, "Checkpoint file cap (0 uses no separate cap)")
	includeSensitive := flags.Bool("include-sensitive", false, "Include common credential-looking filenames")
	requestKey := flags.String("request-key", "", "Persisted capture request key for safe reconciliation")
	resolve := flags.Bool("resolve", false, "Resolve an existing request without capturing")
	timeout := flags.Duration("timeout", 0, "Capture deadline; required with --request-key")
	portable := flags.Bool("portable", false, "Include logical buckets, objects and metadata")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("workspace checkpoint requires --id")
	}
	selection := chosen()
	registry, err := selection.resolve("")
	if err != nil {
		return err
	}
	options := stow.CheckpointOptions{ParentID: *parentID, MaxBytes: *maxBytes, MaxFiles: *maxFiles, IncludeSensitiveFiles: *includeSensitive, PortableObjects: *portable}
	if *requestKey != "" || *resolve {
		return checkpointRequestCommand(registry, *id, checkpointRequestFlags{Key: *requestKey, Resolve: *resolve, Timeout: *timeout, Options: options})
	}
	checkpoint, err := stow.CheckpointOf(context.Background(), registry, *id, options)
	if err != nil {
		return err
	}
	return writeWorkspaceJSON(checkpointResult{
		Version: 1, ID: checkpoint.ID, WorkspaceID: checkpoint.WorkspaceID,
		ParentID: checkpoint.ParentID, Created: checkpoint.Created.Format(time.RFC3339Nano),
		Files: checkpoint.Files, Bytes: checkpoint.Bytes, Excluded: checkpoint.Excluded,
	})
}

func diffWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace diff", flag.ContinueOnError)
	fromID := flags.String("from", "", "Starting checkpoint ID")
	toID := flags.String("to", "", "Ending checkpoint ID")
	chosen := registryFlag(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *fromID == "" || *toID == "" {
		return errors.New("workspace diff requires --from and --to")
	}
	selection := chosen()
	registry, err := selection.resolve("")
	if err != nil {
		return err
	}
	changes, err := stow.CompareCheckpoints(registry, *fromID, *toID)
	if err != nil {
		return err
	}
	return writeWorkspaceJSON(struct {
		Version int                     `json:"version"`
		Changes []stow.CheckpointChange `json:"changes"`
	}{Version: 1, Changes: changes})
}

func restoreCheckpointCommand(args []string) error {
	flags := flag.NewFlagSet("workspace restore", flag.ContinueOnError)
	checkpointID := flags.String("checkpoint-id", "", "Checkpoint ID to restore")
	chosen := registryFlag(flags)
	root := flags.String("root", "", "New workspace root (must not exist; parent must exist)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *checkpointID == "" || *root == "" {
		return errors.New("workspace restore requires --checkpoint-id and --root")
	}
	selection := chosen()
	registry, err := selection.resolve("")
	if err != nil {
		return err
	}
	ws, err := stow.RestoreCheckpoint(registry, *checkpointID, stow.WorkspaceOptions{
		Dir: *root, RegistryDir: registry,
	})
	if err != nil {
		return err
	}
	defer ws.Close()
	return writeWorkspaceJSON(makeWorkspaceResult(ws, 0, 0))
}
