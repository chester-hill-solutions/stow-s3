package mcpstorage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type exportInput struct {
	WorkspaceID  string `json:"workspace_id"`
	CheckpointID string `json:"checkpoint_id"`
	Bundle       string `json:"bundle"`
}

type adoptInput struct {
	Bundle      string `json:"bundle"`
	Destination string `json:"destination"`
}

func (a *adapter) configureRoots() error {
	for _, root := range []*string{&a.config.ExportRoot, &a.config.AdoptRoot} {
		if *root == "" {
			continue
		}
		absolute, err := filepath.Abs(*root)
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return fmt.Errorf("configured root must exist: %w", err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("configured root must be a directory")
		}
		*root = resolved
	}
	return nil
}

func (a *adapter) addHandoffTools(server *mcp.Server) {
	if a.config.ExportRoot != "" {
		mcp.AddTool(server, &mcp.Tool{Name: "stow_handoff_export", Description: "Export a checkpoint to a new named bundle in the configured transfer directory. No automatic retries."}, a.export)
	}
	if a.config.ExportRoot != "" && a.config.AdoptRoot != "" {
		mcp.AddTool(server, &mcp.Tool{Name: "stow_handoff_adopt", Description: "Adopt a named transfer bundle into a new directory under the configured destination root. Receiver grants storage access; no commands run."}, a.adopt)
	}
}

func directChild(root, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:") || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("destination must be a visible direct child name")
	}
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink destination refused")
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func (a *adapter) export(ctx context.Context, _ *mcp.CallToolRequest, input exportInput) (*mcp.CallToolResult, result, error) {
	if err := a.scope(input.WorkspaceID); err != nil {
		return finish(result{}, err)
	}
	if _, err := a.checkpoint(input.CheckpointID); err != nil {
		return finish(result{}, err)
	}
	destination, err := directChild(a.config.ExportRoot, input.Bundle)
	if err != nil {
		return finish(result{}, err)
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()
	handoff, err := stow.ExportHandoff(ctx, stow.HandoffExportOptions{RegistryDir: a.config.RegistryDir, WorkspaceID: input.WorkspaceID, CheckpointID: input.CheckpointID, BundleDir: destination, ArchiveOptions: stow.CheckpointArchiveOptions{MaxBytes: a.config.MaxBytes, MaxFiles: a.config.MaxFiles}})
	return finish(result{Outcome: "committed", Handoff: &handoff, Bundle: input.Bundle}, err)
}

func (a *adapter) adopt(ctx context.Context, _ *mcp.CallToolRequest, input adoptInput) (*mcp.CallToolResult, result, error) {
	_, err := directChild(a.config.ExportRoot, input.Bundle)
	if err != nil {
		return finish(result{}, err)
	}
	destination, err := directChild(a.config.AdoptRoot, input.Destination)
	if err != nil {
		return finish(result{}, err)
	}
	root, err := rooted.Open(a.config.ExportRoot)
	if err != nil {
		return finish(result{}, err)
	}
	defer root.Close()
	reference, err := root.OpenRegularFile(filepath.Join(input.Bundle, "handoff.json"))
	if err != nil {
		return finish(result{}, err)
	}
	defer reference.Close()
	document, err := readBundleDocument(reference)
	if err != nil {
		return finish(result{}, err)
	}
	archive, err := root.OpenRegularFile(filepath.Join(input.Bundle, "checkpoint.tar.gz"))
	if err != nil {
		return finish(result{}, err)
	}
	defer archive.Close()
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()
	ws, cp, err := stow.AdoptHandoffArchive(ctx, document, archive, stow.WorkspaceOptions{Dir: destination, RegistryDir: a.config.RegistryDir}, stow.CheckpointArchiveOptions{MaxBytes: a.config.MaxBytes, MaxFiles: a.config.MaxFiles})
	out := result{Outcome: "committed", OriginCheckpointID: cp.ID}
	if err != nil {
		response, value, finishErr := finish(out, err)
		if cp.ID != "" {
			value.Outcome = "partial_import"
			return finish(value, nil)
		}
		return response, value, finishErr
	}
	defer ws.Close()
	a.mu.Lock()
	a.adopted[ws.ID()] = true
	a.mu.Unlock()
	out.Workspace = &workspaceView{ID: ws.ID(), Directory: ws.Dir(), Bucket: ws.Bucket(), MaxCheckpointBytes: ws.MaxCheckpointBytes(), MaxCheckpoints: ws.MaxCheckpoints()}
	return finish(out, nil)
}

func readBundleDocument(file *os.File) (stow.Handoff, error) {
	info, err := file.Stat()
	if err != nil {
		return stow.Handoff{}, err
	}
	if !info.Mode().IsRegular() {
		return stow.Handoff{}, fmt.Errorf("bundle must contain regular files")
	}
	document, err := stow.ReadHandoffReader(file)
	if err != nil {
		return document, err
	}
	if document.Team != "" {
		return document, fmt.Errorf("bundle team cannot change the configured registry scope")
	}
	if document.Archive == nil || document.Archive.Path != "checkpoint.tar.gz" {
		return document, fmt.Errorf("only self-contained bundle archives are accepted")
	}
	return document, nil
}
