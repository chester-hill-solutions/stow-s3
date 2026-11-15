package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/mcpstorage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPWorkspaceEditModeValidation(t *testing.T) {
	valid := []string{"--workspace-files", "--workspace-edit", "--write-root", "docs", "--coordination", "host_quiesced"}
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "valid edit", args: valid},
		{name: "explicit recovery", args: append(append([]string(nil), valid...), "--recover-file-saves")},
		{name: "explicitly disabled edit", args: []string{"--workspace-edit=false"}},
		{name: "requires workspace files", args: []string{"--workspace-edit", "--write-root", "docs", "--coordination", "host_quiesced"}, want: true},
		{name: "requires write root", args: []string{"--workspace-files", "--workspace-edit", "--coordination", "host_quiesced"}, want: true},
		{name: "requires coordination", args: []string{"--workspace-files", "--workspace-edit", "--write-root", "docs"}, want: true},
		{name: "rejects wrong coordination", args: []string{"--workspace-files", "--workspace-edit", "--write-root", "docs", "--coordination", "best_effort"}, want: true},
		{name: "rejects empty write root", args: []string{"--workspace-files", "--workspace-edit", "--write-root", "", "--coordination", "host_quiesced"}, want: true},
		{name: "rejects resources only", args: append(append([]string(nil), valid...), "--resources-only"), want: true},
		{name: "rejects selected checkpoint", args: append(append([]string(nil), valid...), "--checkpoint-id", "cp_1"), want: true},
		{name: "rejects browser", args: append(append([]string(nil), valid...), "--browser"), want: true},
		{name: "rejects object mode", args: append(append([]string(nil), valid...), "--object-dir", "/tmp/objects"), want: true},
		{name: "write root needs edit", args: []string{"--write-root", "docs"}, want: true},
		{name: "coordination needs edit", args: []string{"--coordination", "host_quiesced"}, want: true},
		{name: "recovery needs edit", args: []string{"--recover-file-saves"}, want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := parseMCPMode(testCase.args)
			if (err != nil) != testCase.want {
				t.Fatalf("parseMCPMode(%v) error=%v, wantError=%v", testCase.args, err, testCase.want)
			}
		})
	}
}

func parseMCPMode(args []string) (*mcpCommandOptions, error) {
	flags := flag.NewFlagSet("mcp-test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := registerMCPFlags(flags)
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	_, err := validateMCPMode(flags)
	return options, err
}

func TestMCPWorkspaceEditInvalidModeHasNoFilesystemEffects(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "never-created")
	err := mcpCommand([]string{"--registry-dir", registry, "--workspace-id", "ws_0000000000000000",
		"--timeout", "10s", "--workspace-files", "--workspace-edit", "--write-root", "docs", "--coordination", "wrong"})
	if err == nil {
		t.Fatal("invalid editing mode was accepted")
	}
	if _, err := os.Lstat(registry); !os.IsNotExist(err) {
		t.Fatalf("invalid mode created registry: %v", err)
	}
}

func TestMCPWorkspaceEditRefusesLiveOwnerBeforeMetadataWrites(t *testing.T) {
	fixture := newWorkspaceEditFixture(t)
	owner, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: fixture.root, RegistryDir: fixture.registry})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	paths := []string{
		filepath.Join(fixture.root, ".stow", "manifest.json"),
		filepath.Join(fixture.root, ".stow", "index.json"),
		filepath.Join(fixture.root, "docs", "note.txt"),
		filepath.Join(fixture.registry, owner.ID()+".json"),
	}
	before := snapshotFiles(t, paths)
	options := &mcpCommandOptions{workspaceFiles: true, workspaceEdit: true, coordination: "host_quiesced",
		writeRoots: []string{"docs"}, workspace: mcpstorage.Config{RegistryDir: fixture.registry,
			WorkspaceID: owner.ID(), ReadRoots: []string{"docs"}, MaxBytes: 1 << 20, MaxFiles: 100, Timeout: 10 * time.Second}}
	if _, _, err := mcpServerFor(false, options); err == nil {
		t.Fatal("editing attached a second writer beside the live owner")
	}
	if after := snapshotFiles(t, paths); !reflect.DeepEqual(before, after) {
		t.Fatal("refused edit startup changed manifest, index, registry or payload")
	}
}

func TestMCPWorkspaceEditStdioSaveAndExplicitRecoveryRestart(t *testing.T) {
	fixture := newWorkspaceEditFixture(t)
	owner, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: fixture.root, RegistryDir: fixture.registry})
	if err != nil {
		t.Fatal(err)
	}
	fixture.workspaceID = owner.ID()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	process := startWorkspaceEditProcess(t, fixture, false)
	assertWorkspaceEditToolCount(t, process.session, 9)
	observed := callWorkspaceEditTool(t, process.session, "stow_workspace_file_read_for_save", map[string]string{"path": "docs/note.txt"})
	if observed.Outcome != "observed" || observed.ObservationToken == "" || observed.DataBase64 == "" {
		t.Fatalf("observation = %+v", observed)
	}
	saved := callWorkspaceEditTool(t, process.session, "stow_workspace_file_save", map[string]string{
		"path": "docs/note.txt", "text": "saved by stdio", "request_key": "cli-save-1", "observation_token": observed.ObservationToken,
	})
	if saved.Outcome != "committed" || saved.Generation == "" || saved.Object == nil {
		t.Fatalf("save = %+v", saved)
	}
	closeWorkspaceEditProcess(t, process)

	recovery := startWorkspaceEditProcess(t, fixture, true)
	assertWorkspaceEditToolCount(t, recovery.session, 9)
	resolved := callWorkspaceEditTool(t, recovery.session, "stow_workspace_file_resolve_save", map[string]string{
		"path": "docs/note.txt", "request_key": "cli-save-1",
	})
	if resolved.Outcome != "committed" || !resolved.Replayed || resolved.Generation != saved.Generation || !bytes.Equal(resolved.Object, saved.Object) {
		t.Fatalf("recovered result = %+v, original=%+v", resolved, saved)
	}
	closeWorkspaceEditProcess(t, recovery)
	readOnlyRecovery, err := stow.ResumeWorkspaceForFileRecovery(stow.WorkspaceOptions{RegistryDir: fixture.registry}, fixture.workspaceID)
	if err != nil {
		t.Fatalf("MCP process retained the owner session: %v", err)
	}
	_ = readOnlyRecovery.Close()
}

type workspaceEditFixture struct {
	root, registry, workspaceID string
}

func newWorkspaceEditFixture(t *testing.T) workspaceEditFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	registry := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("initial bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspaceEditFixture{root: root, registry: registry}
}

type workspaceEditProcess struct {
	command *exec.Cmd
	session *mcp.ClientSession
	stderr  *syncBuffer
	closed  bool
}

type workspaceEditResult struct {
	Outcome          string                 `json:"outcome"`
	ObservationToken string                 `json:"observation_token"`
	DataBase64       string                 `json:"data_base64"`
	Generation       string                 `json:"generation"`
	Replayed         bool                   `json:"replayed"`
	Object           json.RawMessage        `json:"object"`
	Error            *struct{ Code string } `json:"error"`
}

func startWorkspaceEditProcess(t *testing.T, fixture workspaceEditFixture, recover bool) *workspaceEditProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	extra := []string{"--workspace-files", "--workspace-edit", "--write-root", "docs", "--coordination", "host_quiesced", "--read-root", "docs"}
	if recover {
		extra = append(extra, "--recover-file-saves")
	}
	args := append([]string{"mcp", "--registry-dir", fixture.registry, "--workspace-id", fixture.workspaceID, "--timeout", "10s"}, extra...)
	process := &workspaceEditProcess{stderr: &syncBuffer{}, command: exec.Command(launcherBinary(t), args...)}
	process.command.Stderr = process.stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "stow-workspace-edit-cli-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: process.command}, nil)
	if err != nil {
		t.Fatalf("start editing MCP process: %v; stderr=%s", err, process.stderr.String())
	}
	process.session = session
	t.Cleanup(func() {
		if !process.closed {
			_ = session.Close()
		}
	})
	return process
}

func closeWorkspaceEditProcess(t *testing.T, process *workspaceEditProcess) {
	t.Helper()
	if err := process.session.Close(); err != nil {
		t.Fatalf("close editing MCP process: %v; stderr=%s", err, process.stderr.String())
	}
	process.closed = true
	if process.command.ProcessState == nil || !process.command.ProcessState.Success() {
		t.Fatalf("editing MCP process did not exit cleanly: %v; stderr=%s", process.command.ProcessState, process.stderr.String())
	}
}

func callWorkspaceEditTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]string) workspaceEditResult {
	t.Helper()
	response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	for _, content := range response.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			var output workspaceEditResult
			if err := json.Unmarshal([]byte(text.Text), &output); err != nil {
				t.Fatalf("decode %s: %v (%s)", name, err, text.Text)
			}
			return output
		}
	}
	t.Fatalf("tool %s returned no JSON text", name)
	return workspaceEditResult{}
}

func assertWorkspaceEditToolCount(t *testing.T, session *mcp.ClientSession, want int) {
	t.Helper()
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil || len(listed.Tools) != want {
		t.Fatalf("editing tools=%+v err=%v want=%d", listed, err, want)
	}
	if want == 9 {
		names := make(map[string]bool)
		for _, tool := range listed.Tools {
			names[tool.Name] = true
		}
		for _, name := range []string{"stow_workspace_file_read_for_save", "stow_workspace_file_save", "stow_workspace_file_resolve_save", "stow_workspace_file_release_observation"} {
			if !names[name] {
				t.Fatalf("missing editing tool %q: %v", name, names)
			}
		}
	}
}

func snapshotFiles(t *testing.T, paths []string) map[string][]byte {
	t.Helper()
	snapshot := make(map[string][]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshot[path] = data
	}
	return snapshot
}
