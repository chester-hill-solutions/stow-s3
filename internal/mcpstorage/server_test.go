package mcpstorage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixture(t *testing.T) (*mcp.ClientSession, *stow.Workspace, Config) {
	t.Helper()
	config := Config{RegistryDir: filepath.Join(t.TempDir(), "registry"), MaxBytes: 1 << 20, MaxFiles: 100, Timeout: time.Second * 10}
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "work"), RegistryDir: config.RegistryDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	config.WorkspaceID = ws.ID()
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "stow-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, ws, config
}

func invoke(t *testing.T, session *mcp.ClientSession, name string, args json.RawMessage) result {
	t.Helper()
	response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(response.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out result
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != 1 || response.IsError != (out.Error != nil) {
		t.Fatalf("bad envelope: %s", data)
	}
	text, ok := response.Content[0].(*mcp.TextContent)
	var fromText result
	if !ok || json.Unmarshal([]byte(text.Text), &fromText) != nil || !reflect.DeepEqual(fromText, out) {
		t.Fatal("structured/text outputs disagree")
	}
	return out
}

func TestToolSchemas(t *testing.T) {
	session, _, _ := fixture(t)
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil || len(listed.Tools) != 5 {
		t.Fatalf("tools: %v, %v", listed, err)
	}
	for _, tool := range listed.Tools {
		response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: json.RawMessage(`{"unexpected":true}`)})
		if err == nil && !response.IsError {
			t.Fatalf("%s accepted unknown input", tool.Name)
		}
	}
}

func TestToolsCaptureReplayScopeAndGuidance(t *testing.T) {
	session, ws, _ := fixture(t)
	if err := os.WriteFile(filepath.Join(ws.Dir(), "progress.md"), []byte("next: continue"), 0600); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(captureInput{WorkspaceID: ws.ID(), RequestKey: "pilot-request-one"})
	if err != nil {
		t.Fatal(err)
	}
	first := invoke(t, session, "stow_checkpoint_create", args)
	if first.Outcome != "committed" || first.Capture == nil || first.Capture.Replayed {
		t.Fatalf("capture: %+v", first)
	}
	replay := invoke(t, session, "stow_checkpoint_resolve", args)
	if replay.Outcome != "committed" || !replay.Capture.Replayed || replay.Capture.Checkpoint.ID != first.Capture.Checkpoint.ID {
		t.Fatalf("resolve: %+v", replay)
	}
	bad := invoke(t, session, "stow_workspace_inspect", json.RawMessage(`{"workspace_id":"other"}`))
	if bad.Error == nil {
		t.Fatal("scope escape accepted")
	}
	resources, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "stow://guides/checkpoints/v1"})
	if err != nil || len(resources.Contents) != 1 || resources.Contents[0].Text != Guide {
		t.Fatalf("guide: %v, %v", resources, err)
	}
}

func TestLimitsPaginationAndLiveness(t *testing.T) {
	session, ws, config := fixture(t)
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(ws.Dir(), name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cp, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(inspectInput{CheckpointID: cp.ID, Limit: 1})
	page := invoke(t, session, "stow_checkpoint_inspect", data)
	if len(page.Entries) != 1 || page.NextCursor == "" {
		t.Fatalf("page: %+v", page)
	}
	data, _ = json.Marshal(inspectInput{CheckpointID: cp.ID, Limit: 100, Cursor: page.NextCursor})
	rest := invoke(t, session, "stow_checkpoint_inspect", data)
	if len(rest.Entries) != 2 || rest.NextCursor != "" {
		t.Fatalf("page: %+v", rest)
	}
	data, _ = json.Marshal(captureInput{WorkspaceID: ws.ID(), RequestKey: "over-limit", MaxBytes: config.MaxBytes + 1})
	if got := invoke(t, session, "stow_checkpoint_create", data); got.Error == nil {
		t.Fatal("widened bounds")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stow.LookupWorkspace(config.RegistryDir, ws.ID()); err != nil {
		t.Fatal("connection close removed workspace", err)
	}
}

func TestCursorAndConfigurationRefusals(t *testing.T) {
	a := &adapter{config: Config{RegistryDir: "registry", WorkspaceID: "scope"}}
	entries := []entry{{Kind: "file"}, {Kind: "file"}}
	_, token, err := a.page(entries, "one", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.page(entries, "two", token, 1); err == nil {
		t.Fatal("cross-checkpoint cursor accepted")
	}
	for _, limit := range []int{-1, 1001} {
		if _, _, err := a.page(entries, "one", "", limit); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	for _, token := range []string{"!", base64Cursor(t, cursor{Identity: "wrong", Offset: -1})} {
		if _, err := cursorOffset(token, "expected", 2); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
	if _, err := New(Config{}); err == nil {
		t.Fatal("unscoped config accepted")
	}
}

func base64Cursor(t *testing.T, value cursor) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
