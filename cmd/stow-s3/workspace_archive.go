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
	registryDir := flags.String("registry-dir", "", "Workspace registry directory")
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
	if err := exportCheckpointFile(*registryDir, *id, *output, stow.CheckpointArchiveOptions{
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
	absolute, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return fmt.Errorf("archive destination already exists: %s", absolute)
	} else if !os.IsNotExist(err) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(absolute), ".stow-checkpoint-export-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := stow.ExportCheckpoint(context.Background(), registryDir, id, temp, options); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempName, absolute); err != nil {
		return fmt.Errorf("publish checkpoint archive: %w", err)
	}
	return nil
}

func importCheckpointCommand(args []string) error {
	flags := flag.NewFlagSet("workspace import", flag.ContinueOnError)
	archivePath := flags.String("archive", "", "Checkpoint archive file")
	registryDir := flags.String("registry-dir", "", "Workspace registry directory")
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
	file, err := os.Open(*archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	checkpoint, err := stow.ImportCheckpoint(context.Background(), *registryDir, file, stow.CheckpointArchiveOptions{
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
func checkpointWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace checkpoint", flag.ContinueOnError)
	id := flags.String("id", "", "Workspace ID to checkpoint")
	registryDir := flags.String("registry-dir", "", "Workspace registry directory")
	parentID := flags.String("parent", "", "Parent checkpoint ID")
	maxBytes := flags.Int64("max-bytes", 0, "Checkpoint byte cap (0 uses no separate cap)")
	maxFiles := flags.Int64("max-files", 0, "Checkpoint file cap (0 uses no separate cap)")
	includeSensitive := flags.Bool("include-sensitive", false, "Include common credential-looking filenames")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("workspace checkpoint requires --id")
	}
	ws, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: *registryDir}, *id)
	if err != nil {
		return err
	}
	defer ws.Close()
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{
		ParentID: *parentID, MaxBytes: *maxBytes, MaxFiles: *maxFiles,
		IncludeSensitiveFiles: *includeSensitive,
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

func diffWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace diff", flag.ContinueOnError)
	fromID := flags.String("from", "", "Starting checkpoint ID")
	toID := flags.String("to", "", "Ending checkpoint ID")
	registryDir := flags.String("registry-dir", "", "Workspace registry directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *fromID == "" || *toID == "" {
		return errors.New("workspace diff requires --from and --to")
	}
	changes, err := stow.CompareCheckpoints(*registryDir, *fromID, *toID)
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
	registryDir := flags.String("registry-dir", "", "Workspace registry directory")
	root := flags.String("root", "", "New workspace root (must not exist; parent must exist)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *checkpointID == "" || *root == "" {
		return errors.New("workspace restore requires --checkpoint-id and --root")
	}
	ws, err := stow.RestoreCheckpoint(*registryDir, *checkpointID, stow.WorkspaceOptions{
		Dir: *root, RegistryDir: *registryDir,
	})
	if err != nil {
		return err
	}
	defer ws.Close()
	return writeWorkspaceJSON(makeWorkspaceResult(ws, 0, 0))
}
