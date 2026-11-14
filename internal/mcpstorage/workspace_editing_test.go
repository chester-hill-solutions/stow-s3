package mcpstorage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func workspaceEditingFixture(t *testing.T, roots []string) (*WorkspaceFiles, *mcp.ClientSession, Config) {
	t.Helper()
	registry := filepath.Join(t.TempDir(), "registry")
	root := filepath.Join(t.TempDir(), "work")
	owner, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	config := Config{RegistryDir: registry, WorkspaceID: owner.ID(), ReadRoots: roots, MaxBytes: 1 << 20, MaxFiles: 100, Timeout: 5 * time.Second}
	host, err := NewWorkspaceEditing(config, stow.WorkspaceEditOptions{Coordination: "host_quiesced", WriteRoots: []string{"docs"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	session, closeSessions := workspaceResourceSession(t, host)
	t.Cleanup(closeSessions)
	return host, session, config
}

func invokeWorkspaceFile(t *testing.T, session *mcp.ClientSession, name string, input any) workspaceFileToolResult {
	t.Helper()
	args, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(response)
	if err != nil || len(wire) > maxResponseBytes {
		t.Fatalf("response bytes=%d error=%v", len(wire), err)
	}
	body, err := json.Marshal(response.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var result workspaceFileToolResult
	if err := json.Unmarshal(body, &result); err != nil || result.Version != 1 || response.IsError != (result.Error != nil) {
		t.Fatalf("invalid response=%s err=%v", body, err)
	}
	var text workspaceFileToolResult
	content, ok := response.Content[0].(*mcp.TextContent)
	if !ok || json.Unmarshal([]byte(content.Text), &text) != nil || text.Outcome != result.Outcome || text.Error != nil != (result.Error != nil) {
		t.Fatal("text and structured workspace result disagree")
	}
	return result
}

func TestWorkspaceEditingToolsSaveResolveAndRelease(t *testing.T) {
	host, session, config := workspaceEditingFixture(t, []string{"docs"})
	assertWorkspaceEditingToolsAndOverview(t, host, session)
	assertWorkspaceEditingSaveLifecycle(t, session)
	assertWorkspaceEditingEmptyRelease(t, session)
	if host.owner.ID() != host.workspaceID {
		t.Fatal("editing owner identity changed")
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := stow.ResumeWorkspaceForFileRecovery(stow.WorkspaceOptions{RegistryDir: config.RegistryDir}, host.workspaceID)
	if err != nil {
		t.Fatalf("resume after editor close=%v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceEditingSaveReceiptRetainsRelativePathAndMetadata(t *testing.T) {
	registry, root := filepath.Join(t.TempDir(), "registry"), filepath.Join(t.TempDir(), "work")
	owner, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]string{"origin": "seed", "purpose": "receipt-test"}
	if _, err := owner.PutObject(t.Context(), owner.Bucket(), "docs/note.txt", []byte("before"), stow.PutOptions{Metadata: metadata}); err != nil {
		t.Fatal(err)
	}
	id := owner.ID()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	config := Config{RegistryDir: registry, WorkspaceID: id, ReadRoots: []string{"docs"}, MaxBytes: 1 << 20, MaxFiles: 100, Timeout: 5 * time.Second}
	host, err := NewWorkspaceEditing(config, stow.WorkspaceEditOptions{Coordination: "host_quiesced", WriteRoots: []string{"docs"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	session, closeSessions := workspaceResourceSession(t, host)
	t.Cleanup(closeSessions)
	observed := invokeWorkspaceFile(t, session, "stow_workspace_file_read_for_save", workspaceFileReadInput{Path: "docs/note.txt"})
	saved := invokeWorkspaceFile(t, session, "stow_workspace_file_save", workspaceFileSaveInput{Path: observed.Path,
		Text: stringPointer("updated"), RequestKey: "metadata-save", ObservationToken: observed.ObservationToken})
	assertReceiptMetadata(t, saved, "updated")
	saved.Object.Metadata["origin"] = "caller-mutated"
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("later bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := invokeWorkspaceFile(t, session, "stow_workspace_file_resolve_save", workspaceFileResolveInput{Path: "docs/note.txt", RequestKey: "metadata-save"})
	if !resolved.Replayed || resolved.Outcome != string(stow.SaveCommitted) {
		t.Fatalf("receipt replay=%+v", resolved)
	}
	assertReceiptMetadata(t, resolved, "updated")
}

func assertReceiptMetadata(t *testing.T, result workspaceFileToolResult, expectedBody string) {
	t.Helper()
	if result.Error != nil || result.Object == nil || result.Object.Path != "docs/note.txt" || result.Object.Metadata["origin"] != "seed" || result.Object.Metadata["purpose"] != "receipt-test" || result.Object.Size != int64(len(expectedBody)) {
		t.Fatalf("saved object metadata=%+v", result)
	}
}

func assertWorkspaceEditingToolsAndOverview(t *testing.T, host *WorkspaceFiles, session *mcp.ClientSession) {
	t.Helper()
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil || len(listed.Tools) != 9 {
		t.Fatalf("tools=%v err=%v", listed, err)
	}
	if err := host.EnableBrowser(); err == nil {
		t.Fatal("editing host enabled browser profile")
	}
	overview, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "stow://workspaces/" + host.workspaceID + "/v1"})
	var view workspaceOverview
	if err != nil || json.Unmarshal([]byte(overview.Contents[0].Text), &view) != nil || view.ReadOnly || view.Coordination != "host_quiesced" || len(view.WriteRoots) != 1 || view.WriteRoots[0] != "docs" {
		t.Fatalf("editing overview=%+v err=%v", view, err)
	}
}

func assertWorkspaceEditingSaveLifecycle(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	observed := invokeWorkspaceFile(t, session, "stow_workspace_file_read_for_save", workspaceFileReadInput{Path: "docs/note.txt"})
	if observed.Error != nil || observed.Text == nil || *observed.Text != "before" || observed.DataBase64 == nil || observed.ObservationToken == "" {
		t.Fatalf("read result=%+v", observed)
	}
	saveInput := workspaceFileSaveInput{Path: observed.Path, Text: stringPointer("after"), RequestKey: "request-one", ObservationToken: observed.ObservationToken}
	saved := invokeWorkspaceFile(t, session, "stow_workspace_file_save", saveInput)
	if saved.Error != nil || saved.Outcome != string(stow.SaveCommitted) || !saved.Available || saved.Object == nil || saved.Object.Size != 5 {
		t.Fatalf("save result=%+v", saved)
	}
	resolved := invokeWorkspaceFile(t, session, "stow_workspace_file_resolve_save", workspaceFileResolveInput{Path: saveInput.Path, RequestKey: saveInput.RequestKey})
	if resolved.Error != nil || resolved.Outcome != string(stow.SaveCommitted) || !resolved.Replayed || resolved.Generation != saved.Generation {
		t.Fatalf("resolve result=%+v", resolved)
	}
}

func assertWorkspaceEditingEmptyRelease(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	empty := invokeWorkspaceFile(t, session, "stow_workspace_file_read_for_save", workspaceFileReadInput{Path: "docs/empty.txt"})
	if empty.Error != nil || empty.DataBase64 == nil || *empty.DataBase64 != "" || empty.Text == nil || *empty.Text != "" {
		t.Fatalf("empty read=%+v", empty)
	}
	released := invokeWorkspaceFile(t, session, "stow_workspace_file_release_observation", workspaceFileReleaseInput{ObservationToken: empty.ObservationToken})
	if released.Error != nil || !released.Released {
		t.Fatalf("release=%+v", released)
	}
	invalid := invokeWorkspaceFile(t, session, "stow_workspace_file_release_observation", workspaceFileReleaseInput{ObservationToken: empty.ObservationToken})
	if invalid.Error == nil || invalid.Error.Code != "invalid_observation" {
		t.Fatalf("repeat release=%+v", invalid)
	}
	absent := invokeWorkspaceFile(t, session, "stow_workspace_file_read_for_save", workspaceFileReadInput{Path: "docs/missing.txt"})
	wire, _ := json.Marshal(absent)
	if absent.Error != nil || !absent.Absent || absent.Mode == nil || *absent.Mode != 0 || !bytes.Contains(wire, []byte(`"mode":0`)) {
		t.Fatalf("zero-mode absence response=%s result=%+v", wire, absent)
	}
	_ = invokeWorkspaceFile(t, session, "stow_workspace_file_release_observation", workspaceFileReleaseInput{ObservationToken: absent.ObservationToken})
}

func TestWorkspaceEditingPayloadBoundsAndScopes(t *testing.T) {
	host, session, _ := workspaceEditingFixture(t, []string{"docs"})
	before := workspaceEditingStateSnapshot(t, host)
	outside := invokeWorkspaceFile(t, session, "stow_workspace_file_read_for_save", workspaceFileReadInput{Path: "other.txt"})
	if outside.Error == nil || outside.Error.Code != "unavailable" || outside.ObservationToken != "" || strings.Contains(outside.Error.Message, host.owner.Dir()) {
		t.Fatalf("out of scope read=%+v", outside)
	}
	invalid := invokeWorkspaceFile(t, session, "stow_workspace_file_save", workspaceFileSaveInput{Path: "docs/new.txt", DataBase64: stringPointer("%%%"), RequestKey: "bad", Replace: true})
	if invalid.Error == nil || invalid.Error.Code != "invalid" || invalid.Outcome != string(stow.SaveNotCommitted) {
		t.Fatalf("invalid base64=%+v", invalid)
	}
	large := invokeWorkspaceFile(t, session, "stow_workspace_file_save", workspaceFileSaveInput{Path: "docs/new.txt", Text: stringPointer(strings.Repeat("x", maxObjectPayload+1)), RequestKey: "large", Replace: true})
	if large.Error == nil || large.Error.Code != "payload_too_large" || large.Outcome != string(stow.SaveNotCommitted) {
		t.Fatalf("large save=%+v", large)
	}
	assertWorkspaceEditingState(t, host, before)
}

func TestWorkspaceEditingRejectsLiveOwnerWithoutChangingSource(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	root := filepath.Join(t.TempDir(), "work")
	owner, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "source.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, ".stow", "manifest.json"), filepath.Join(root, ".stow", "index.json"),
		filepath.Join(registry, owner.ID()+".json"), filepath.Join(root, "docs", "source.txt")}
	before := snapshotWorkspaceFiles(t, paths)
	config := Config{RegistryDir: registry, WorkspaceID: owner.ID(), MaxBytes: 1 << 20, MaxFiles: 100, Timeout: time.Second}
	_, err = NewWorkspaceEditing(config, stow.WorkspaceEditOptions{Coordination: "host_quiesced", WriteRoots: []string{"docs"}}, false)
	if err == nil || strings.Contains(err.Error(), root) {
		t.Fatalf("live owner refusal=%v", err)
	}
	assertWorkspaceFilesEqual(t, paths, before)
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceEditingRequiresExplicitMarkedSaveRecovery(t *testing.T) {
	config, request := pendingWorkspaceEditingFixture(t)
	edit := stow.WorkspaceEditOptions{Coordination: "host_quiesced", WriteRoots: []string{"docs"}}
	if _, err := NewWorkspaceEditing(config, edit, false); err == nil {
		t.Fatal("ordinary editing accepted pending save marker")
	}
	host, err := NewWorkspaceEditing(config, edit, true)
	if err != nil {
		t.Fatalf("explicit recovery editing=%v", err)
	}
	t.Cleanup(func() { _ = host.Close() })
	session, closeSessions := workspaceResourceSession(t, host)
	t.Cleanup(closeSessions)
	resolved := invokeWorkspaceFile(t, session, "stow_workspace_file_resolve_save", workspaceFileResolveInput{Path: request.Path, RequestKey: request.RequestKey})
	if resolved.Error != nil || resolved.Outcome != string(stow.SaveCommitted) {
		t.Fatalf("recovered request=%+v", resolved)
	}
}

func pendingWorkspaceEditingFixture(t *testing.T) (Config, stow.WorkspaceFileSaveRequest) {
	t.Helper()
	registry, root := filepath.Join(t.TempDir(), "registry"), filepath.Join(t.TempDir(), "work")
	owner, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	editor, err := owner.OpenFileEditor(stow.WorkspaceEditOptions{Coordination: "host_quiesced", ReadRoots: []string{"docs"}, WriteRoots: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = editor.Close() })
	observation, err := editor.ReadForSave(context.Background(), "docs/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	request := stow.WorkspaceFileSaveRequest{Path: observation.Path, Data: []byte("new"), RequestKey: "pending-request", ObservationToken: observation.ObservationToken}
	indexPath := filepath.Join(root, ".stow", "index.json")
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.SaveFile(context.Background(), request); !errors.Is(err, stow.ErrSaveRequestUnknown) {
		t.Fatalf("seed pending save error=%v", err)
	}
	if err := editor.Close(); err != nil {
		t.Fatal(err)
	}
	id := owner.ID()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, indexBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{RegistryDir: registry, WorkspaceID: id, ReadRoots: []string{"docs"}, MaxBytes: 1 << 20, MaxFiles: 100, Timeout: 5 * time.Second}, request
}

func TestWorkspaceEditingLargeReadsKeepBase64AndDropOptionalText(t *testing.T) {
	host, session, _ := workspaceEditingFixture(t, []string{"docs"})
	data := make([]byte, maxObjectPayload)
	for index := range data {
		data[index] = byte(index % 127)
	}
	if err := os.WriteFile(filepath.Join(host.owner.Dir(), "docs", "max.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	result := invokeWorkspaceFile(t, session, "stow_workspace_file_read_for_save", workspaceFileReadInput{Path: "docs/max.bin"})
	decoded, err := base64.StdEncoding.DecodeString(pointerValue(result.DataBase64))
	if err != nil || len(decoded) != maxObjectPayload || result.Text != nil {
		t.Fatalf("bounded large read: bytes=%d text=%t err=%v", len(decoded), result.Text != nil, err)
	}
	data[len(data)-1] = 0xff
	if err := os.WriteFile(filepath.Join(host.owner.Dir(), "docs", "binary.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := invokeWorkspaceFile(t, session, "stow_workspace_file_read_for_save", workspaceFileReadInput{Path: "docs/binary.bin"})
	decoded, err = base64.StdEncoding.DecodeString(pointerValue(binary.DataBase64))
	if err != nil || len(decoded) != maxObjectPayload || decoded[len(decoded)-1] != 0xff || binary.Text != nil {
		t.Fatalf("bounded binary read: bytes=%d text=%t err=%v", len(decoded), binary.Text != nil, err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	saved := invokeWorkspaceFile(t, session, "stow_workspace_file_save", workspaceFileSaveInput{Path: "docs/roundtrip.bin", DataBase64: &encoded, RequestKey: "binary-max", Replace: true})
	if saved.Error != nil || saved.Outcome != string(stow.SaveCommitted) || saved.Object == nil || saved.Object.Size != maxObjectPayload {
		t.Fatalf("max binary save=%+v", saved)
	}
}

func TestWorkspaceEditingRawArgumentBounds(t *testing.T) {
	host, session, _ := workspaceEditingFixture(t, []string{"docs"})
	before := workspaceEditingStateSnapshot(t, host)
	assertWorkspaceEditingRawArgumentRefused(t, session)
	assertWorkspaceEditingState(t, host, before)
	assertWorkspaceEditingSDKErrorSanitized(t, session)
}

func assertWorkspaceEditingRawArgumentRefused(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	oversized, _ := json.Marshal(map[string]string{"path": strings.Repeat("x", maxResponseBytes)})
	response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "stow_workspace_file_save", Arguments: json.RawMessage(oversized)})
	if err != nil || response == nil || !response.IsError {
		t.Fatalf("raw argument response=%v err=%v", response, err)
	}
	var rejected workspaceFileToolResult
	body, _ := json.Marshal(response.StructuredContent)
	if json.Unmarshal(body, &rejected) != nil || rejected.Error == nil || rejected.Error.Code != "payload_too_large" || rejected.Outcome != string(stow.SaveNotCommitted) || strings.Contains(rejected.Error.Message, strings.Repeat("x", 128)) {
		t.Fatalf("raw argument error is not sanitized: %+v", rejected)
	}
}

func assertWorkspaceEditingSDKErrorSanitized(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "stow_workspace_file_save", Arguments: json.RawMessage(`{"unknown":"value"}`)})
	if err != nil || response == nil || !response.IsError {
		t.Fatalf("invalid SDK arguments response=%v err=%v", response, err)
	}
	body, _ := json.Marshal(response.StructuredContent)
	var rejected workspaceFileToolResult
	if json.Unmarshal(body, &rejected) != nil || rejected.Error == nil || rejected.Error.Code != "invalid" || strings.Contains(rejected.Error.Message, "value") {
		t.Fatalf("SDK validation error is not sanitized: %+v", rejected)
	}
}

func TestWorkspaceEditingTerminalResponseBounds(t *testing.T) {
	large := strings.Repeat("x", maxResponseBytes)
	data := large
	value := workspaceFileToolResult{Outcome: "committed", Replayed: true, Committed: true, Generation: "generation-1", DataBase64: &data}
	original, err := encodeWorkspaceFileTool(value)
	if err != nil {
		t.Fatal(err)
	}
	boundedResult, err := boundWorkspaceEditResult(original)
	response, ok := boundedResult.(*mcp.CallToolResult)
	if err != nil || !ok || response == nil {
		t.Fatalf("bounded response=%v err=%v", boundedResult, err)
	}
	body, _ := json.Marshal(response.StructuredContent)
	var bounded workspaceFileToolResult
	decodeErr := json.Unmarshal(body, &bounded)
	if !response.IsError || decodeErr != nil || bounded.Outcome != "committed" || !bounded.Replayed || !bounded.Committed || bounded.Generation != "generation-1" || bounded.Error == nil || bounded.Error.Code != "response_too_large" {
		t.Fatalf("terminal bound=%+v err=%v", bounded, err)
	}
}

func stringPointer(value string) *string { return &value }

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func snapshotWorkspaceFiles(t *testing.T, paths []string) [][]byte {
	t.Helper()
	values := make([][]byte, len(paths))
	for index, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		values[index] = data
	}
	return values
}

type workspaceEditingState struct {
	payload, index    []byte
	identity, pending bool
}

func workspaceEditingStateSnapshot(t *testing.T, host *WorkspaceFiles) workspaceEditingState {
	t.Helper()
	root := host.owner.Dir()
	payload, err := os.ReadFile(filepath.Join(root, "docs", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(root, ".stow", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity := workspaceEditingPathExists(t, filepath.Join(root, ".stow", "file-saves", "identity.json"))
	pending := workspaceEditingPathExists(t, filepath.Join(root, ".stow", "file-saves", "pending.json"))
	return workspaceEditingState{payload: payload, index: index, identity: identity, pending: pending}
}

func workspaceEditingPathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatal(err)
	return false
}

func assertWorkspaceEditingState(t *testing.T, host *WorkspaceFiles, want workspaceEditingState) {
	t.Helper()
	got := workspaceEditingStateSnapshot(t, host)
	if !bytes.Equal(got.payload, want.payload) || !bytes.Equal(got.index, want.index) || got.identity != want.identity || got.pending != want.pending {
		t.Fatal("preflight refusal changed workspace files or save journal")
	}
}

func assertWorkspaceFilesEqual(t *testing.T, paths []string, expected [][]byte) {
	t.Helper()
	for index, path := range paths {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected[index]) {
			t.Fatalf("workspace file changed at %s: err=%v", filepath.Base(path), err)
		}
	}
}
