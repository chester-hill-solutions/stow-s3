package mcpstorage

import (
	"context"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type workspaceInput struct {
	WorkspaceID string `json:"workspace_id"`
}

type captureInput struct {
	WorkspaceID string `json:"workspace_id"`
	RequestKey  string `json:"request_key"`
	ParentID    string `json:"parent_id,omitempty"`
	MaxBytes    int64  `json:"max_bytes,omitempty"`
	MaxFiles    int64  `json:"max_files,omitempty"`
}

func (a *adapter) inspectWorkspace(_ context.Context, _ *mcp.CallToolRequest, input workspaceInput) (*mcp.CallToolResult, result, error) {
	if err := a.scope(input.WorkspaceID); err != nil {
		return finish(result{}, err)
	}
	ref, err := stow.LookupWorkspace(a.config.RegistryDir, input.WorkspaceID)
	if err != nil {
		return finish(result{}, err)
	}
	return finish(result{Outcome: "ok", Workspace: &workspaceView{ID: ref.ID, Directory: ref.Dir, Bucket: ref.Bucket, MaxCheckpointBytes: ref.MaxCheckpointBytes, MaxCheckpoints: ref.MaxCheckpoints}}, nil)
}

func (a *adapter) request(input captureInput) (stow.CheckpointRequest, error) {
	if err := a.scope(input.WorkspaceID); err != nil {
		return stow.CheckpointRequest{}, err
	}
	if input.MaxBytes < 0 || input.MaxFiles < 0 || input.MaxBytes > a.config.MaxBytes || input.MaxFiles > a.config.MaxFiles {
		return stow.CheckpointRequest{}, fmt.Errorf("capture limits must narrow the configured bounds")
	}
	if input.MaxBytes == 0 {
		input.MaxBytes = a.config.MaxBytes
	}
	if input.MaxFiles == 0 {
		input.MaxFiles = a.config.MaxFiles
	}
	return stow.CheckpointRequest{Key: input.RequestKey, Options: stow.CheckpointOptions{ParentID: input.ParentID, MaxBytes: input.MaxBytes, MaxFiles: input.MaxFiles, PortableObjects: true}}, nil
}

func (a *adapter) capture(ctx context.Context, _ *mcp.CallToolRequest, input captureInput) (*mcp.CallToolResult, result, error) {
	request, err := a.request(input)
	if err != nil {
		return finish(result{}, err)
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()
	saved, err := stow.CaptureCheckpoint(ctx, a.config.RegistryDir, input.WorkspaceID, request)
	return finish(result{Outcome: saved.Outcome, Capture: &saved}, err)
}

func (a *adapter) resolve(ctx context.Context, _ *mcp.CallToolRequest, input captureInput) (*mcp.CallToolResult, result, error) {
	request, err := a.request(input)
	if err != nil {
		return finish(result{}, err)
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()
	saved, err := stow.ResolveCheckpoint(ctx, a.config.RegistryDir, input.WorkspaceID, request)
	return finish(result{Outcome: saved.Outcome, Capture: &saved}, err)
}
