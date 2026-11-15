package mcpstorage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkspaceFilesResourceOnlyAndNavigation(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := os.Mkdir(filepath.Join(ws.Dir(), "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "docs", "readme.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{RegistryDir: registry, WorkspaceID: ws.ID(), MaxBytes: 1 << 20, MaxFiles: 100, Timeout: 5 * time.Second}
	host, err := NewWorkspaceFiles(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	session, closeSessions := workspaceResourceSession(t, host)
	defer closeSessions()
	resources := assertResourceOnly(t, session)
	assertWorkspaceNavigation(t, session, ws.ID())
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: resources.Resources[0].URI}); err == nil {
		t.Fatal("overview remained available after close")
	}
}

func workspaceResourceSession(t *testing.T, host *WorkspaceFiles) (*mcp.ClientSession, func()) {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := host.Server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "workspace-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatal(err)
	}
	return session, func() { _ = session.Close(); _ = serverSession.Close() }
}

func assertResourceOnly(t *testing.T, session *mcp.ClientSession) *mcp.ListResourcesResult {
	t.Helper()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 0 {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	prompts, err := session.ListPrompts(context.Background(), nil)
	if err != nil || len(prompts.Prompts) != 0 {
		t.Fatalf("prompts=%v err=%v", prompts, err)
	}
	resources, err := session.ListResources(context.Background(), nil)
	if err != nil || len(resources.Resources) != 1 {
		t.Fatalf("resources=%v err=%v", resources, err)
	}
	return resources
}

func assertWorkspaceNavigation(t *testing.T, session *mcp.ClientSession, id string) {
	t.Helper()
	rootURI := "stow://workspaces/" + id + "/tree/" + base64.RawURLEncoding.EncodeToString([]byte("."))
	tree, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: rootURI})
	if err != nil {
		t.Fatal(err)
	}
	var directory workspaceDirectory
	if err := json.Unmarshal([]byte(tree.Contents[0].Text), &directory); err != nil {
		t.Fatal(err)
	}
	if !directory.Complete || len(directory.Entries) != 1 || directory.Entries[0].Kind != "directory" {
		t.Fatalf("directory=%+v", directory)
	}
	fileURI := workspacePathURI("files", id, "docs/readme.txt")
	file, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: fileURI})
	if err != nil || len(file.Contents) != 1 || file.Contents[0].Text != "hello" {
		t.Fatalf("file=%+v err=%v", file, err)
	}
}

func TestWorkspaceResourceURIRejectsNoncanonicalScope(t *testing.T) {
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
	uris := []string{
		"stow://workspaces/" + ws.ID() + "/files/YS8vYi",
		"stow://workspaces/other/files/YQ",
		workspacePathURI("files", ws.ID(), ".env"),
		workspacePathURI("files", ws.ID(), "missing.txt"),
		"stow://workspaces/" + ws.ID() + "/files/YQ==",
		"stow://workspaces/" + ws.ID() + "/files/YQ?query",
		"other://workspaces/" + ws.ID() + "/files/YQ",
	}
	want := ""
	for _, uri := range uris {
		_, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err == nil {
			t.Errorf("URI unexpectedly succeeded: %q", uri)
			continue
		}
		if want == "" {
			want = err.Error()
		}
		if err.Error() != want {
			t.Errorf("URI %q error=%q, want common category %q", uri, err, want)
		}
	}
}
