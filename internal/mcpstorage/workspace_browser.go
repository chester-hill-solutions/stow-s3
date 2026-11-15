package mcpstorage

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const workspaceBrowserURI = "ui://stow/workspace-browser-v1"

type workspaceBrowserLaunch struct {
	Mode        string `json:"mode"`
	OverviewURI string `json:"overview_uri"`
}

func (host *WorkspaceFiles) EnableBrowser() error {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.checkpointReader != nil {
		return fmt.Errorf("workspace browser unavailable for checkpoint resources")
	}
	if host.resourcesOnly {
		return fmt.Errorf("workspace browser unavailable in resource-only mode")
	}
	if host.editing {
		return fmt.Errorf("workspace browser unavailable in editing mode")
	}
	if host.closed {
		return fmt.Errorf("workspace reader closed")
	}
	if host.browserEnabled {
		return nil
	}
	closedWorld := false
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld}
	host.Server.AddResource(&mcp.Resource{URI: workspaceBrowserURI, Name: "Stow workspace browser", MIMEType: browserMIME}, host.readWorkspaceBrowserUI)
	mcp.AddTool(host.Server, &mcp.Tool{
		Name:        "stow_workspace_browser",
		Title:       "Stow workspace browser",
		Description: "Browse the configured workspace through bounded read-only resources.",
		Annotations: annotations,
		Meta:        mcp.Meta{"ui": browserToolUI{ResourceURI: workspaceBrowserURI}, "openai/ui": browserEntrypoints{Entrypoints: []browserEntry{{Type: "global"}, {Type: "thread"}}}},
	}, host.launchWorkspaceBrowser)
	host.browserEnabled = true
	return nil
}

func (host *WorkspaceFiles) launchWorkspaceBrowser(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, workspaceBrowserLaunch, error) {
	_, done, err := host.begin(ctx, host.timeout)
	if err != nil {
		return nil, workspaceBrowserLaunch{}, err
	}
	defer done()
	launch := workspaceBrowserLaunch{Mode: "workspace", OverviewURI: "stow://workspaces/" + host.resourceWorkspaceID() + "/v1"}
	return &mcp.CallToolResult{Content: []mcp.Content{}}, launch, nil
}

func (host *WorkspaceFiles) readWorkspaceBrowserUI(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	readCtx, done, err := host.begin(ctx, host.timeout)
	if err != nil {
		return nil, err
	}
	defer done()
	select {
	case <-readCtx.Done():
		return nil, fmt.Errorf("workspace browser unavailable")
	default:
	}
	uri := "stow://workspaces/" + host.resourceWorkspaceID() + "/v1"
	html, err := workspaceBrowserHTML(uri)
	if err != nil {
		return nil, err
	}
	return browserUIResult(workspaceBrowserURI, html)
}
