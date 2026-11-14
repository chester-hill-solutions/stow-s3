package mcpstorage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkspaceBrowserRegistrationAndBootstrap(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	config := Config{RegistryDir: registry, WorkspaceID: ws.ID(), MaxBytes: 1, MaxFiles: 1, Timeout: time.Second}
	assertBrowserEnableRefused(t, config)
	host, err := NewWorkspaceFiles(config, false)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	session, closeSessions := workspaceResourceSession(t, host)
	defer closeSessions()
	assertWorkspaceToolCount(t, session, 5)
	if err := host.EnableBrowser(); err != nil {
		t.Fatal(err)
	}
	if err := host.EnableBrowser(); err != nil {
		t.Fatalf("repeat enable: %v", err)
	}
	assertWorkspaceToolCount(t, session, 6)
	assertWorkspaceBrowserTool(t, session, ws.ID())
	assertWorkspaceBrowserUI(t, session, ws.ID(), ws.Dir(), registry)
	if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ui://stow/other"}); err == nil {
		t.Fatal("unknown UI URI succeeded")
	}
}

func assertBrowserEnableRefused(t *testing.T, config Config) {
	t.Helper()
	resourcesOnly, err := NewWorkspaceFiles(config, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := resourcesOnly.EnableBrowser(); err == nil {
		t.Fatal("resource-only browser enable succeeded")
	}
	_ = resourcesOnly.Close()
	if err := resourcesOnly.EnableBrowser(); err == nil {
		t.Fatal("closed browser enable succeeded")
	}
}

func assertWorkspaceBrowserTool(t *testing.T, session *mcp.ClientSession, id string) {
	t.Helper()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := findWorkspaceTool(t, tools.Tools, "stow_workspace_browser")
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
		t.Fatalf("tool annotations=%+v", tool.Annotations)
	}
	if tool.Meta["ui"] == nil || tool.Meta["openai/ui"] == nil {
		t.Fatalf("UI metadata=%v", tool.Meta)
	}
	call, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "stow_workspace_browser"})
	if err != nil {
		t.Fatal(err)
	}
	var launch workspaceBrowserLaunch
	encoded, err := json.Marshal(call.StructuredContent)
	if err != nil || json.Unmarshal(encoded, &launch) != nil {
		t.Fatalf("structured launch=%s err=%v", encoded, err)
	}
	if launch.Mode != "workspace" || launch.OverviewURI != "stow://workspaces/"+id+"/v1" {
		t.Fatalf("launch=%+v", launch)
	}
}

func assertWorkspaceBrowserUI(t *testing.T, session *mcp.ClientSession, id, workspaceDir, registry string) {
	t.Helper()
	resources, err := session.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	foundUI := false
	for _, resource := range resources.Resources {
		foundUI = foundUI || resource.URI == workspaceBrowserURI
	}
	if len(resources.Resources) != 3 || !foundUI {
		t.Fatalf("resources=%+v", resources.Resources)
	}
	ui, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: workspaceBrowserURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(ui.Contents) != 1 || ui.Contents[0].MIMEType != browserMIME {
		t.Fatalf("UI result=%+v", ui)
	}
	text := ui.Contents[0].Text
	if !strings.Contains(text, `<script type="application/json" id="stow-bootstrap">{"mode":"workspace","overview_uri":"stow://workspaces/`+id+`/v1"}</script>`) {
		t.Fatal("workspace bootstrap missing")
	}
	if strings.Contains(text, workspaceDir) || strings.Contains(text, registry) {
		t.Fatal("workspace UI disclosed host paths")
	}
}

func assertWorkspaceToolCount(t *testing.T, session *mcp.ClientSession, expected int) {
	t.Helper()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != expected {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
}

func findWorkspaceTool(t *testing.T, tools []*mcp.Tool, name string) *mcp.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func TestWorkspaceBrowserURIUnavailableUnlessEnabled(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	host, err := NewWorkspaceFiles(Config{RegistryDir: registry, WorkspaceID: ws.ID(), MaxBytes: 1, MaxFiles: 1, Timeout: time.Second}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	session, closeSessions := workspaceResourceSession(t, host)
	defer closeSessions()
	if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: workspaceBrowserURI}); err == nil {
		t.Fatal("unconfigured UI resource succeeded")
	}
	if got, err := browserHTML(); err != nil || len(got) == 0 || len(got) > 2<<20 || len(browserCompressed) > 512<<10 {
		t.Fatalf("bundle sizes html=%d compressed=%d err=%v", len(got), len(browserCompressed), err)
	}
}
