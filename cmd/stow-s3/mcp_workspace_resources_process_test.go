package main

import (
	"context"
	"encoding/base64"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/mcpstorage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type workspaceMCPProcess struct {
	command *exec.Cmd
	session *mcp.ClientSession
	ctx     context.Context
	closed  bool
}

type inProcessHTTPTransport struct{ handler http.Handler }

func (transport inProcessHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

func startWorkspaceMCPProcess(t *testing.T, registry, id string, extra ...string) *workspaceMCPProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	args := append([]string{"mcp", "--registry-dir", registry, "--workspace-id", id, "--timeout", "10s"}, extra...)
	process := &workspaceMCPProcess{command: exec.Command(launcherBinary(t), args...), ctx: ctx}
	client := mcp.NewClient(&mcp.Implementation{Name: "workspace-files-cli-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: process.command}, nil)
	if err != nil {
		t.Fatalf("start workspace MCP: %v", err)
	}
	process.session = session
	t.Cleanup(func() {
		if !process.closed {
			_ = session.Close()
		}
	})
	return process
}

func closeWorkspaceMCPProcess(t *testing.T, process *workspaceMCPProcess) {
	t.Helper()
	if err := process.session.Close(); err != nil {
		t.Fatalf("workspace MCP shutdown: %v", err)
	}
	process.closed = true
	if process.command.ProcessState == nil || !process.command.ProcessState.Success() {
		t.Fatalf("workspace MCP exit state=%v", process.command.ProcessState)
	}
}

func TestMCPWorkspaceResourceFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--resources-only"}, "requires --workspace-files"},
		{[]string{"--workspace-files", "--object-dir", t.TempDir()}, "incompatible"},
		{[]string{"--resources-only", "--workspace-files", "--bucket", "b"}, "incompatible"},
		{[]string{"--read-root", "inputs"}, "requires --workspace-files"},
		{[]string{"--browser"}, "requires --workspace-files"},
		{[]string{"--workspace-files", "--browser", "--resources-only"}, "incompatible"},
		{[]string{"--object-dir", t.TempDir(), "--read-root", "inputs"}, "incompatible"},
	} {
		flags := flag.NewFlagSet("mcp-test", flag.ContinueOnError)
		options := registerMCPFlags(flags)
		if err := flags.Parse(tc.args); err != nil {
			t.Fatal(err)
		}
		_, err := validateMCPMode(flags)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("args %v: error=%v, want %q", tc.args, err, tc.want)
		}
		_ = options
	}
	flags := flag.NewFlagSet("mcp-test", flag.ContinueOnError)
	options := registerMCPFlags(flags)
	if err := flags.Parse([]string{"--workspace-files", "--read-root", "inputs", "--read-root", "docs"}); err != nil {
		t.Fatal(err)
	}
	if _, err := validateMCPMode(flags); err != nil {
		t.Fatalf("valid workspace roots: %v", err)
	}
	options.workspace.ReadRoots = append(options.workspace.ReadRoots, options.readRoots...)
	if strings.Join(options.workspace.ReadRoots, ",") != "inputs,docs" {
		t.Fatalf("read roots=%v", options.workspace.ReadRoots)
	}
}

func TestMCPWorkspaceFilesBrowserProcess(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, directory := range []string{"inputs", "inputs-private"} {
		if err := os.Mkdir(filepath.Join(ws.Dir(), directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "inputs", "notes.txt"), []byte("read me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "inputs-private", "secret.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	process := startWorkspaceMCPProcess(t, registry, ws.ID(), "--workspace-files", "--browser", "--read-root", "inputs")
	tools, err := process.session.ListTools(process.ctx, nil)
	if err != nil || len(tools.Tools) != 6 {
		t.Fatalf("workspace browser tools=%v err=%v", tools, err)
	}
	fileURI := "stow://workspaces/" + ws.ID() + "/files/" + base64.RawURLEncoding.EncodeToString([]byte("inputs/notes.txt"))
	file, err := process.session.ReadResource(process.ctx, &mcp.ReadResourceParams{URI: fileURI})
	if err != nil || file.Contents[0].Text != "read me" {
		t.Fatalf("read scoped file=%+v err=%v", file, err)
	}
	privateURI := "stow://workspaces/" + ws.ID() + "/files/" + base64.RawURLEncoding.EncodeToString([]byte("inputs-private/secret.txt"))
	if _, err := process.session.ReadResource(process.ctx, &mcp.ReadResourceParams{URI: privateURI}); err == nil {
		t.Fatal("read root widened to sibling prefix")
	}
	ui, err := process.session.ReadResource(process.ctx, &mcp.ReadResourceParams{URI: "ui://stow/workspace-browser-v1"})
	if err != nil || !strings.Contains(ui.Contents[0].Text, `"mode":"workspace"`) {
		t.Fatalf("workspace UI=%+v err=%v", ui, err)
	}
	closeWorkspaceMCPProcess(t, process)
}

func TestMCPWorkspaceFilesProcessAndReaderLease(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(ws.Dir(), "notes.txt")
	if err := os.WriteFile(filePath, []byte("host-written"), 0600); err != nil {
		t.Fatal(err)
	}
	process := startWorkspaceMCPProcess(t, registry, ws.ID(), "--workspace-files")
	tools, err := process.session.ListTools(process.ctx, nil)
	if err != nil || len(tools.Tools) != 5 {
		t.Fatalf("normal workspace tools=%v err=%v", tools, err)
	}
	treeURI := "stow://workspaces/" + ws.ID() + "/tree/" + base64.RawURLEncoding.EncodeToString([]byte("."))
	tree, err := process.session.ReadResource(process.ctx, &mcp.ReadResourceParams{URI: treeURI})
	if err != nil || len(tree.Contents) != 1 || !strings.Contains(tree.Contents[0].Text, "notes.txt") {
		t.Fatalf("read root tree=%+v err=%v", tree, err)
	}
	fileURI := "stow://workspaces/" + ws.ID() + "/files/" + base64.RawURLEncoding.EncodeToString([]byte("notes.txt"))
	file, err := process.session.ReadResource(process.ctx, &mcp.ReadResourceParams{URI: fileURI})
	if err != nil || file.Contents[0].Text != "host-written" {
		t.Fatalf("read file=%+v err=%v", file, err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, ws.ID()); err == nil {
		t.Fatal("destroy succeeded while MCP reader was attached")
	}
	closeWorkspaceMCPProcess(t, process)
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, ws.ID()); err != nil {
		t.Fatalf("destroy after reader close: %v", err)
	}
}

func TestMCPWorkspaceResourcesOnlyProcessAndKillRelease(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	id := ws.ID()
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	process := startWorkspaceMCPProcess(t, registry, id, "--workspace-files", "--resources-only")
	if _, err := process.session.ListTools(process.ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := process.session.CallTool(process.ctx, &mcp.CallToolParams{Name: "stow_checkpoint_create"}); err == nil {
		t.Fatal("resource-only process exposed checkpoint tools")
	}
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, id); err == nil {
		t.Fatal("destroy succeeded while resource-only reader was attached")
	}
	if err := process.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = process.command.Process.Wait()
	process.closed = true
	_ = process.session.Close()
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, id); err != nil {
		t.Fatalf("destroy after process kill: %v", err)
	}
}

func TestMCPWorkspaceHTTPRequestDoesNotCloseReader(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	id := ws.ID()
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	options := &mcpCommandOptions{workspace: mcpstorage.Config{RegistryDir: registry, WorkspaceID: id, MaxBytes: 1024, MaxFiles: 10, Timeout: time.Second}, workspaceFiles: true}
	server, closer, err := mcpServerFor(false, options)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	handler := mcpHTTPHandler(server, "")
	client := mcp.NewClient(&mcp.Implementation{Name: "workspace-http-test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: "http://mcp.test", HTTPClient: &http.Client{Transport: inProcessHTTPTransport{handler: handler}}}
	session, err := client.Connect(t.Context(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "stow://workspaces/" + id + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = session.Close()
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, id); err == nil {
		t.Fatal("HTTP session close released server-owned reader")
	}
	if err := closer(); err != nil {
		t.Fatal(err)
	}
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, id); err != nil {
		t.Fatalf("destroy after server close: %v", err)
	}
}
