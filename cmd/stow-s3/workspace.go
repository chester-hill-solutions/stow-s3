package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type workspaceResult struct {
	Version          int                       `json:"version"`
	WorkspaceID      string                    `json:"workspace_id"`
	Root             string                    `json:"root"`
	WorkingDirectory string                    `json:"working_directory"`
	Bucket           string                    `json:"bucket"`
	CheckpointID     string                    `json:"checkpoint_id,omitempty"`
	RegistryDir      string                    `json:"registry_dir,omitempty"`
	SeededBytes      int64                     `json:"seeded_bytes,omitempty"`
	SeededObjects    int64                     `json:"seeded_objects,omitempty"`
	BaseIdentity     string                    `json:"base_identity,omitempty"`
	Repositories     []stow.PreparedRepository `json:"repositories,omitempty"`
	Capabilities     workspaceCapabilities     `json:"capabilities"`
	Authority        []stow.Operation          `json:"authority"`
}

type workspaceCapabilities struct {
	Backend    stow.Backend `json:"backend"`
	MaxBytes   int64        `json:"max_bytes"`
	MaxObjects int64        `json:"max_objects"`
	Persistent bool         `json:"persistent"`
	Multipart  bool         `json:"multipart"`
	Upstream   bool         `json:"upstream"`
}

type workspaceHandoff struct {
	Version      int    `json:"version"`
	WorkspaceID  string `json:"workspace_id"`
	CheckpointID string `json:"checkpoint_id,omitempty"`
	RegistryDir  string `json:"registry_dir"`
}

type checkpointResult struct {
	Version     int      `json:"version"`
	ID          string   `json:"checkpoint_id"`
	WorkspaceID string   `json:"workspace_id"`
	ParentID    string   `json:"parent_checkpoint_id,omitempty"`
	Created     string   `json:"created"`
	Files       int64    `json:"files"`
	Bytes       int64    `json:"bytes"`
	Excluded    []string `json:"excluded_sensitive_paths,omitempty"`
}

func workspaceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: stow-s3 workspace <prepare|resume|checkpoint|diff|restore|handoff|export|import>")
	}
	switch args[0] {
	case "prepare":
		return prepareWorkspaceCommand(args[1:])
	case "resume":
		return resumeWorkspaceCommand(args[1:])
	case "handoff":
		return handoffWorkspaceCommand(args[1:])
	case "checkpoint":
		return checkpointWorkspaceCommand(args[1:])
	case "diff":
		return diffWorkspaceCommand(args[1:])
	case "restore":
		return restoreCheckpointCommand(args[1:])
	case "export":
		return exportCheckpointCommand(args[1:])
	case "import":
		return importCheckpointCommand(args[1:])
	default:
		return fmt.Errorf("unknown workspace command %q", args[0])
	}
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

func prepareWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace prepare", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "Versioned JSON task manifest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" {
		return errors.New("workspace prepare requires --manifest")
	}
	manifest, err := readWorkspaceManifest(*manifestPath)
	if err != nil {
		return err
	}
	prepared, err := prepareWorkspaceFromManifest(manifest)
	if err != nil {
		return err
	}
	defer prepared.Workspace.Close()
	result := makeWorkspaceResult(prepared.Workspace, prepared.SeededBytes, prepared.SeededObjects)
	result.RegistryDir = manifest.RegistryDir
	if result.RegistryDir == "" {
		result.RegistryDir, err = stow.DefaultWorkspaceRegistryDir()
		if err != nil {
			return err
		}
	}
	result.BaseIdentity = prepared.BaseIdentity
	result.Repositories = prepared.Repositories
	return writeWorkspaceJSON(result)
}

func readWorkspaceManifest(path string) (stow.WorkspaceTaskManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return stow.WorkspaceTaskManifest{}, fmt.Errorf("read task manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest stow.WorkspaceTaskManifest
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode task manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return manifest, errors.New("task manifest must contain exactly one JSON value")
	}
	if manifest.Version != 1 {
		return manifest, fmt.Errorf("unsupported task manifest version %d (supported: 1)", manifest.Version)
	}
	if manifest.MaxBytes < 0 || manifest.MaxObjects < 0 || manifest.TTLSeconds < 0 {
		return manifest, errors.New("task manifest limits and TTL must not be negative")
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return manifest, err
	}
	resolveTaskManifestPaths(base, &manifest)
	return manifest, nil
}

func resolveTaskManifestPaths(base string, manifest *stow.WorkspaceTaskManifest) {
	manifest.Root = resolveManifestPath(base, manifest.Root)
	if manifest.RegistryDir != "" {
		manifest.RegistryDir = resolveManifestPath(base, manifest.RegistryDir)
	}
	for i := range manifest.Inputs {
		manifest.Inputs[i].Source = resolveManifestPath(base, manifest.Inputs[i].Source)
	}
	for i := range manifest.Repositories {
		manifest.Repositories[i].Source = resolveManifestPath(base, manifest.Repositories[i].Source)
	}
	if manifest.Root == base || manifest.Root == "." {
		manifest.Root = ""
	}
}

func prepareWorkspaceFromManifest(manifest stow.WorkspaceTaskManifest) (*stow.PreparedWorkspace, error) {
	if manifest.Root == "" {
		return nil, errors.New("task manifest root must name a new dedicated directory")
	}
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{
			Dir: manifest.Root, Bucket: manifest.Bucket,
			MaxBytes: manifest.MaxBytes, MaxObjects: manifest.MaxObjects,
			TTL:         time.Duration(manifest.TTLSeconds) * time.Second,
			RegistryDir: manifest.RegistryDir,
		},
		WorkingDirectory:       manifest.WorkingDirectory,
		Inputs:                 manifest.Inputs,
		Repositories:           manifest.Repositories,
		IncludeSensitiveInputs: manifest.IncludeSensitiveInputs,
	})
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func resumeWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace resume", flag.ContinueOnError)
	id := flags.String("id", "", "Workspace ID to resume")
	handoffPath := flags.String("handoff", "", "Read a same-machine workspace handoff reference")
	registryDir := flags.String("registry-dir", "", "Workspace registry directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedID, resolvedRegistryDir, checkpointID, err := resumeIdentifiers(*id, *handoffPath, *registryDir)
	if err != nil {
		return err
	}
	*id, *registryDir = resolvedID, resolvedRegistryDir
	if *id == "" {
		return errors.New("workspace resume requires --id or --handoff")
	}
	if *registryDir != "" {
		absolute, err := filepath.Abs(*registryDir)
		if err != nil {
			return err
		}
		*registryDir = absolute
	}
	ws, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: *registryDir}, *id)
	if err != nil {
		return err
	}
	defer ws.Close()
	if checkpointID != "" {
		manifest, err := stow.LoadCheckpoint(*registryDir, checkpointID)
		if err != nil {
			return err
		}
		if manifest.WorkspaceID != ws.ID() {
			return errors.New("handoff checkpoint belongs to a different workspace")
		}
	}
	result := makeWorkspaceResult(ws, 0, 0)
	result.CheckpointID = checkpointID
	result.RegistryDir = *registryDir
	if result.RegistryDir == "" {
		result.RegistryDir, err = stow.DefaultWorkspaceRegistryDir()
		if err != nil {
			return err
		}
	}
	return writeWorkspaceJSON(result)
}

func resumeIdentifiers(id, handoffPath, registryDir string) (string, string, string, error) {
	if handoffPath == "" {
		return id, registryDir, "", nil
	}
	if id != "" || registryDir != "" {
		return "", "", "", errors.New("workspace resume accepts --handoff or --id/--registry-dir, not both")
	}
	data, err := os.ReadFile(handoffPath)
	if err != nil {
		return "", "", "", err
	}
	var handoff workspaceHandoff
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&handoff); err != nil {
		return "", "", "", fmt.Errorf("decode handoff reference: %w", err)
	}
	if handoff.Version != 1 || handoff.WorkspaceID == "" || handoff.RegistryDir == "" {
		return "", "", "", errors.New("invalid or unsupported workspace handoff reference")
	}
	return handoff.WorkspaceID, handoff.RegistryDir, handoff.CheckpointID, nil
}

func handoffWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace handoff", flag.ContinueOnError)
	id := flags.String("id", "", "Workspace ID to hand off")
	registryDir := flags.String("registry-dir", "", "Workspace registry directory")
	checkpointID := flags.String("checkpoint-id", "", "Optional immutable checkpoint to include")
	output := flags.String("output", "", "Write the local handoff reference to this file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("workspace handoff requires --id")
	}
	ws, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: *registryDir}, *id)
	if err != nil {
		return err
	}
	defer ws.Close()
	if *checkpointID != "" {
		manifest, err := stow.LoadCheckpoint(*registryDir, *checkpointID)
		if err != nil {
			return err
		}
		if manifest.WorkspaceID != ws.ID() {
			return errors.New("checkpoint belongs to a different workspace")
		}
	}
	registry := *registryDir
	if registry == "" {
		registry, err = stow.DefaultWorkspaceRegistryDir()
		if err != nil {
			return err
		}
	} else {
		registry, err = filepath.Abs(registry)
		if err != nil {
			return err
		}
	}
	return writeWorkspaceJSONTo(*output, workspaceHandoff{Version: 1, WorkspaceID: ws.ID(), CheckpointID: *checkpointID, RegistryDir: registry})
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

func makeWorkspaceResult(ws *stow.Workspace, seededBytes, seededObjects int64) workspaceResult {
	capabilities := ws.Capabilities()
	return workspaceResult{
		Version: 1, WorkspaceID: ws.ID(), Root: ws.Dir(),
		WorkingDirectory: ws.WorkingDirectory(), Bucket: ws.Bucket(),
		SeededBytes: seededBytes, SeededObjects: seededObjects,
		Capabilities: workspaceCapabilities{
			Backend: capabilities.Backend, MaxBytes: capabilities.MaxBytes,
			MaxObjects: capabilities.MaxObjects, Persistent: capabilities.Persistent,
			Multipart: capabilities.Multipart, Upstream: capabilities.Upstream,
		},
		Authority: ws.Authority().Operations(),
	}
}

func writeWorkspaceJSON(value interface{}) error { return writeWorkspaceJSONTo("", value) }

func writeWorkspaceJSONTo(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
