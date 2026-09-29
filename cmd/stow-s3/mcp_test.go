package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPNativeStdioNegotiationAndCapture(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := os.WriteFile(filepath.Join(ws.Dir(), "progress.md"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) { exerciseMCPProcess(t, registry, ws.ID(), version) })
	}
	if _, err := stow.LookupWorkspace(registry, ws.ID()); err != nil {
		t.Fatalf("MCP shutdown removed workspace: %v", err)
	}
}

func exerciseMCPProcess(t *testing.T, registry, id, version string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.Command(launcherBinary(t), "mcp", "--registry-dir", registry, "--workspace-id", id, "--timeout", "10s")
	client := mcp.NewClient(&mcp.Implementation{Name: "stow-cli-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, &mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 5 {
		t.Fatalf("tools: %v %v", listed, err)
	}
	args, err := json.Marshal(struct {
		WorkspaceID string `json:"workspace_id"`
		RequestKey  string `json:"request_key"`
	}{id, "stdio-" + version})
	if err != nil {
		t.Fatal(err)
	}
	captured, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "stow_checkpoint_create", Arguments: json.RawMessage(args)})
	if err != nil || captured.IsError {
		t.Fatalf("capture: %+v %v", captured, err)
	}
	resolved, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "stow_checkpoint_resolve", Arguments: json.RawMessage(args)})
	if err != nil || resolved.IsError {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
}
