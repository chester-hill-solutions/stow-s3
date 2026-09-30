package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type objectMCPProcess struct {
	context context.Context
	session *mcp.ClientSession
	command *exec.Cmd
	stderr  *syncBuffer
	closed  bool
}

type objectMCPView struct {
	Outcome          string             `json:"outcome"`
	ObservationToken string             `json:"observation_token"`
	Absent           bool               `json:"absent"`
	Replayed         bool               `json:"replayed"`
	Object           *objectMCPMetadata `json:"object"`
	Error            *struct {
		Code string `json:"code"`
	} `json:"error"`
}

type objectMCPMetadata struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type"`
	Metadata     map[string]string `json:"metadata"`
	LastModified string            `json:"last_modified"`
	DataBase64   string            `json:"data_base64"`
}

func startObjectMCPProcess(t *testing.T, dir string, extra []string, version string) *objectMCPProcess {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native durable object mode requires Linux or macOS")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	args := append([]string{"mcp", "--object-dir", dir, "--bucket", "objects"}, extra...)
	process := &objectMCPProcess{context: ctx, command: exec.Command(launcherBinary(t), args...), stderr: &syncBuffer{}}
	process.command.Stderr = process.stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "stow-object-cli-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: process.command}, &mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		t.Fatalf("start object MCP: %v; stderr=%s", err, process.stderr.String())
	}
	process.session = session
	t.Cleanup(func() {
		if !process.closed {
			_ = session.Close()
		}
	})
	return process
}

func closeObjectMCPProcess(t *testing.T, process *objectMCPProcess) {
	t.Helper()
	if err := process.session.Close(); err != nil {
		t.Fatalf("object MCP EOF shutdown: %v; stderr=%s", err, process.stderr.String())
	}
	process.closed = true
	if process.command.ProcessState == nil || !process.command.ProcessState.Success() {
		t.Fatalf("object MCP did not exit cleanly: %v; stderr=%s", process.command.ProcessState, process.stderr.String())
	}
}

func callObjectMCP(t *testing.T, process *objectMCPProcess, name string, args map[string]any, wantError bool) objectMCPView {
	t.Helper()
	called, err := process.session.CallTool(process.context, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tool %s transport: %v", name, err)
	}
	if called.IsError != wantError {
		t.Fatalf("tool %s error=%v, expected %v: %+v", name, called.IsError, wantError, called)
	}
	var output objectMCPView
	for _, content := range called.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			if err := json.Unmarshal([]byte(text.Text), &output); err != nil {
				t.Fatalf("tool %s malformed result: %v", name, err)
			}
			return output
		}
	}
	t.Fatalf("tool %s returned no text envelope", name)
	return output
}

func objectSaveArguments(data, requestKey string) map[string]any {
	return map[string]any{"key": "report", "data_base64": base64.StdEncoding.EncodeToString([]byte(data)), "request_key": requestKey}
}

func TestMCPObjectNativeProcessDiscardedReplyAndReopen(t *testing.T) {
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) { exerciseObjectMCPReopen(t, version) })
	}
}

func exerciseObjectMCPReopen(t *testing.T, version string) {
	t.Helper()
	dir := t.TempDir()
	process := startObjectMCPProcess(t, dir, []string{"--create-bucket"}, version)
	assertObjectMCPTools(t, process)
	observed := callObjectMCP(t, process, "stow_object_read_for_save", map[string]any{"key": "report"}, false)
	if !observed.Absent || observed.ObservationToken == "" {
		t.Fatalf("absence observation=%+v", observed)
	}
	args := objectSaveArguments("original", "stdio-discarded-reply")
	args["observation_token"] = observed.ObservationToken
	args["content_type"] = "text/plain"
	args["metadata"] = map[string]string{"reviewed": "yes"}
	// Discarding the application reply simulates lost reply handling; shutdown is orderly.
	_ = callObjectMCP(t, process, "stow_object_save", args, false)
	closeObjectMCPProcess(t, process)
	process = startObjectMCPProcess(t, dir, nil, version)
	resolved := callObjectMCP(t, process, "stow_object_resolve_save", map[string]any{"key": "report", "request_key": "stdio-discarded-reply"}, false)
	if resolved.Outcome != "committed" || resolved.Object == nil {
		t.Fatalf("reopened receipt=%+v", resolved)
	}
	if resolved.Object.Size != 8 || resolved.Object.ContentType != "text/plain" || resolved.Object.Metadata["reviewed"] != "yes" {
		t.Fatalf("original metadata=%+v", resolved.Object)
	}
	assertObjectMCPStaleSaveAndOriginalReceipt(t, process, resolved)
	closeObjectMCPProcess(t, process)
	assertObjectStoreReacquired(t, dir, "later")
}

func assertObjectMCPStaleSaveAndOriginalReceipt(t *testing.T, process *objectMCPProcess, original objectMCPView) {
	t.Helper()
	observation := callObjectMCP(t, process, "stow_object_read_for_save", map[string]any{"key": "report"}, false)
	replace := objectSaveArguments("later", "stdio-later")
	replace["replace"] = true
	replaced := callObjectMCP(t, process, "stow_object_save", replace, false)
	if replaced.Outcome != "committed" {
		t.Fatalf("replacement=%+v", replaced)
	}
	stale := objectSaveArguments("stale", "stdio-stale")
	stale["observation_token"] = observation.ObservationToken
	refused := callObjectMCP(t, process, "stow_object_save", stale, true)
	if refused.Outcome != "not_committed" || refused.Error == nil || refused.Error.Code != "conflict" {
		t.Fatalf("stale save=%+v", refused)
	}
	resolved := callObjectMCP(t, process, "stow_object_resolve_save", map[string]any{"key": "report", "request_key": "stdio-discarded-reply"}, false)
	if !reflect.DeepEqual(resolved.Object, original.Object) {
		t.Fatalf("old receipt changed: before=%+v after=%+v", original.Object, resolved.Object)
	}
	current := callObjectMCP(t, process, "stow_object_read_for_save", map[string]any{"key": "report"}, false)
	if current.Object == nil || current.Object.DataBase64 != base64.StdEncoding.EncodeToString([]byte("later")) {
		t.Fatalf("stale save changed bytes=%+v", current)
	}
}

func assertObjectMCPTools(t *testing.T, process *objectMCPProcess) {
	t.Helper()
	listed, err := process.session.ListTools(process.context, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	expected := []string{"stow_object_read_for_save", "stow_object_save", "stow_object_resolve_save", "stow_object_release_observation", "stow_object_capabilities"}
	if len(names) != len(expected) {
		t.Fatalf("object tools=%v", names)
	}
	for _, name := range expected {
		if !names[name] {
			t.Fatalf("missing tool %s", name)
		}
	}
}

func assertObjectStoreReacquired(t *testing.T, dir, expected string) {
	t.Helper()
	store, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: dir})
	if err != nil {
		t.Fatalf("MCP shutdown retained directory lock: %v", err)
	}
	defer store.Close()
	current, err := store.GetObject(t.Context(), "objects", "report")
	if err != nil || string(current.Data) != expected || store.Usage() != (stow.Usage{Bytes: int64(len(expected)), Objects: 1}) {
		t.Fatalf("current=%+v err=%v usage=%+v", current, err, store.Usage())
	}
}

func TestMCPObjectReadOnlyNativeProcessRefusesSaveAndReleasesDirectory(t *testing.T) {
	dir := t.TempDir()
	process := startObjectMCPProcess(t, dir, []string{"--create-bucket"}, "2026-07-28")
	save := objectSaveArguments("original", "stdio-initial")
	save["replace"] = true
	_ = callObjectMCP(t, process, "stow_object_save", save, false)
	closeObjectMCPProcess(t, process)
	process = startObjectMCPProcess(t, dir, []string{"--read-only"}, "2026-07-28")
	_ = callObjectMCP(t, process, "stow_object_read_for_save", map[string]any{"key": "report"}, false)
	save = objectSaveArguments("denied", "stdio-denied")
	save["replace"] = true
	refused := callObjectMCP(t, process, "stow_object_save", save, true)
	if refused.Error == nil || refused.Error.Code != "denied" || refused.Outcome != "unknown" {
		t.Fatalf("read-only save=%+v", refused)
	}
	closeObjectMCPProcess(t, process)
	assertObjectStoreReacquired(t, dir, "original")
}

func TestMCPNativeObjectHelpVersionAndInvalidModeDispatch(t *testing.T) {
	binary := launcherBinary(t)
	for _, flag := range []string{"--help", "--version"} {
		output, err := exec.Command(binary, "mcp", flag).CombinedOutput()
		if err != nil || len(output) == 0 {
			t.Fatalf("mcp %s: %s %v", flag, output, err)
		}
		if flag == "--help" && !strings.Contains(string(output), "object-dir") {
			t.Fatalf("object mode absent from native help: %s", output)
		}
	}
	dir := t.TempDir() + "/never-opened"
	output, err := exec.Command(binary, "mcp", "--object-dir", dir, "--bucket", "objects", "--timeout", "1s").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "incompatible") {
		t.Fatalf("mixed mode accepted: %s %v", output, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("invalid native dispatch opened directory: %v", err)
	}
}

func TestMCPObjectNativeSignalReleasesDirectory(t *testing.T) {
	dir := t.TempDir()
	process := startObjectMCPProcess(t, dir, []string{"--create-bucket"}, "2026-07-28")
	args := objectSaveArguments("saved", "stdio-signal")
	args["replace"] = true
	result := callObjectMCP(t, process, "stow_object_save", args, false)
	if result.Outcome != "committed" {
		t.Fatalf("initial save=%+v", result)
	}
	if err := process.command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	_ = process.session.Close()
	process.closed = true
	if process.command.ProcessState == nil || !process.command.ProcessState.Exited() {
		t.Fatalf("signaled host did not exit: %v", process.command.ProcessState)
	}
	assertObjectStoreReacquired(t, dir, "saved")
}
