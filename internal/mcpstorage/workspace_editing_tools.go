package mcpstorage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxWorkspaceFilePayloadBytes = storage.MaxWorkspaceFileBytes
const maxWorkspaceFileTextBytes = 64 << 10

type workspaceFileReadInput struct {
	Path string `json:"path"`
}

type workspaceFileSaveInput struct {
	Path             string  `json:"path"`
	Text             *string `json:"text,omitempty"`
	DataBase64       *string `json:"data_base64,omitempty"`
	RequestKey       string  `json:"request_key"`
	ObservationToken string  `json:"observation_token,omitempty"`
	Replace          bool    `json:"replace,omitempty"`
}

type workspaceFileResolveInput struct {
	Path       string `json:"path"`
	RequestKey string `json:"request_key"`
}

type workspaceFileReleaseInput struct {
	ObservationToken string `json:"observation_token"`
}

func (host *WorkspaceFiles) addWorkspaceEditingTools() {
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_workspace_file_read_for_save", Title: "Read a workspace file for saving",
		Description: "Read one file in the configured workspace and return its bytes, digest, mode and a short-lived observation token. Use it before a guarded save, or to see what a tracked file contains; do not use it as a general filesystem reader outside the configured read roots. The token expires, so save promptly and read again if it has. Returns outcome observed with data_base64, text below 64 KiB, sha256 and observation_token; refusals carry a next step.",
		Annotations: readOnlyTool("Read a workspace file for saving")}, host.readWorkspaceFileForSave)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_workspace_file_save", Title: "Save a workspace file",
		Description: "Save exactly one file using the observation token from stow_workspace_file_read_for_save, or replace true to overwrite deliberately. Use it for edits inside the configured write roots; do not use it outside them, and do not retry an uncertain save with a new request key. Persist request_key before calling: retrying with the same key returns the original receipt and never writes twice. Returns outcome committed, not_committed or unknown, the object metadata, and a next step on refusal.",
		Annotations: writeTool("Save a workspace file", true)}, host.saveWorkspaceFile)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_workspace_file_resolve_save", Title: "Resolve a workspace file save",
		Description: "Return the retained result of an earlier workspace save by path and request key, without writing anything. Use it after a timeout, a dropped reply or outcome unknown; do not use it to start a new save or to test whether some other request happened. It is safe to call repeatedly. Returns the same receipt with replayed true.",
		Annotations: readOnlyTool("Resolve a workspace file save")}, host.resolveWorkspaceFileSave)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_workspace_file_release_observation", Title: "Release a workspace file observation",
		Description: "Release a live observation token so it cannot be used for a save and stops counting against the observation limit. Use it when the edit will not happen; do not release a token you are about to save with. Releasing an unknown or expired token is an error, not a no-op. Returns released true; saved receipts are unaffected.",
		Annotations: writeTool("Release a workspace file observation", false)}, host.releaseWorkspaceFileObservation)
}

func (host *WorkspaceFiles) readWorkspaceFileForSave(ctx context.Context, _ *mcp.CallToolRequest, input workspaceFileReadInput) (*mcp.CallToolResult, workspaceFileToolResult, error) {
	readCtx, done, err := host.begin(ctx, host.timeout)
	if err != nil {
		return finishWorkspaceFileTool(workspaceFileToolResult{}, err)
	}
	defer done()
	file, err := host.editor.ReadForSave(readCtx, input.Path)
	if err != nil {
		return finishWorkspaceFileTool(workspaceFileToolResult{}, err)
	}
	dataBase64 := base64.StdEncoding.EncodeToString(file.Data)
	mode := uint32(file.Mode.Perm())
	result := workspaceFileToolResult{Outcome: "observed", Available: true, Path: file.Path, Absent: file.Absent,
		DataBase64: &dataBase64, SHA256: file.SHA256, Mode: &mode,
		Modified: file.Modified, Generation: file.Generation, ObservationToken: file.ObservationToken, ExpiresAt: file.ExpiresAt}
	if utf8.Valid(file.Data) && len(file.Data) < maxWorkspaceFileTextBytes {
		text := string(file.Data)
		result.Text = &text
	}
	response, bounded, encodeErr := finishWorkspaceFileTool(result, nil)
	if bounded.Error != nil {
		_ = host.editor.ReleaseObservation(file.ObservationToken)
	}
	return response, bounded, encodeErr
}

func (host *WorkspaceFiles) saveWorkspaceFile(ctx context.Context, _ *mcp.CallToolRequest, input workspaceFileSaveInput) (*mcp.CallToolResult, workspaceFileToolResult, error) {
	writeCtx, done, err := host.begin(ctx, host.timeout)
	if err != nil {
		return finishWorkspaceFileTool(workspaceFileToolResult{Outcome: string(stow.SaveNotCommitted)}, err)
	}
	defer done()
	data, err := workspaceFileInputBytes(input)
	if err != nil {
		return finishWorkspaceFileTool(workspaceFileToolResult{Outcome: string(stow.SaveNotCommitted)}, err)
	}
	saved, err := host.editor.SaveFile(writeCtx, stow.WorkspaceFileSaveRequest{Path: input.Path, Data: data,
		RequestKey: input.RequestKey, ObservationToken: input.ObservationToken, Replace: input.Replace})
	return finishWorkspaceFileTool(workspaceFileSaveResult(saved), err)
}

func workspaceFileInputBytes(input workspaceFileSaveInput) ([]byte, error) {
	if (input.Text == nil) == (input.DataBase64 == nil) || (input.ObservationToken != "") == input.Replace {
		return nil, workspaceToolError{"invalid"}
	}
	if input.Text != nil {
		if len(*input.Text) > maxWorkspaceFilePayloadBytes {
			return nil, workspaceToolError{"payload_too_large"}
		}
		return []byte(*input.Text), nil
	}
	if len(*input.DataBase64) > base64.StdEncoding.EncodedLen(maxWorkspaceFilePayloadBytes) {
		return nil, workspaceToolError{"payload_too_large"}
	}
	data, err := base64.StdEncoding.Strict().DecodeString(*input.DataBase64)
	if err != nil || len(data) > maxWorkspaceFilePayloadBytes {
		return nil, workspaceToolError{"invalid"}
	}
	return data, nil
}

func (host *WorkspaceFiles) resolveWorkspaceFileSave(ctx context.Context, _ *mcp.CallToolRequest, input workspaceFileResolveInput) (*mcp.CallToolResult, workspaceFileToolResult, error) {
	readCtx, done, err := host.begin(ctx, host.timeout)
	if err != nil {
		return finishWorkspaceFileTool(workspaceFileToolResult{}, err)
	}
	defer done()
	resolved, err := host.editor.ResolveSave(readCtx, input.Path, input.RequestKey)
	return finishWorkspaceFileTool(workspaceFileSaveResult(resolved), err)
}

func (host *WorkspaceFiles) releaseWorkspaceFileObservation(ctx context.Context, _ *mcp.CallToolRequest, input workspaceFileReleaseInput) (*mcp.CallToolResult, workspaceFileToolResult, error) {
	_, done, err := host.begin(ctx, host.timeout)
	if err != nil {
		return finishWorkspaceFileTool(workspaceFileToolResult{}, err)
	}
	defer done()
	if err := host.editor.ReleaseObservation(input.ObservationToken); err != nil {
		return finishWorkspaceFileTool(workspaceFileToolResult{}, err)
	}
	return finishWorkspaceFileTool(workspaceFileToolResult{Outcome: "released", Available: true, Released: true}, nil)
}

func (host *WorkspaceFiles) limitWorkspaceEditCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		return workspaceEditMiddlewareCall(next, ctx, method, request)
	}
}

func workspaceEditMiddlewareCall(next mcp.MethodHandler, ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
	name := workspaceFileCallName(method, request)
	if name == "" {
		return next(ctx, method, request)
	}
	if workspaceEditArgumentsTooLarge(request) {
		if name == "stow_workspace_file_save" {
			return workspaceEditSaveErrorResult("payload_too_large")
		}
		return workspaceEditErrorResult("payload_too_large")
	}
	result, err := next(ctx, method, request)
	if err != nil {
		return workspaceEditErrorResult("invalid")
	}
	return boundWorkspaceEditResult(result)
}

func workspaceFileCallName(method string, request mcp.Request) string {
	call, ok := request.(*mcp.CallToolRequest)
	if method != "tools/call" || !ok || call.Params == nil || !workspaceFileToolName(call.Params.Name) {
		return ""
	}
	return call.Params.Name
}

func workspaceEditArgumentsTooLarge(request mcp.Request) bool {
	call := request.(*mcp.CallToolRequest)
	return len(call.Params.Arguments) > maxWorkspaceFileResponseBytes || len(call.Params.Name) > 128
}

func workspaceEditErrorResult(code string) (mcp.Result, error) {
	result, _, err := finishWorkspaceFileTool(workspaceFileToolResult{}, workspaceToolError{code})
	return result, err
}

func workspaceEditSaveErrorResult(code string) (mcp.Result, error) {
	value := workspaceFileToolResult{Outcome: string(stow.SaveNotCommitted)}
	result, _, err := finishWorkspaceFileTool(value, workspaceToolError{code})
	return result, err
}

func boundWorkspaceEditResult(result mcp.Result) (mcp.Result, error) {
	tool, ok := result.(*mcp.CallToolResult)
	if !ok {
		return result, nil
	}
	if tool.IsError && !workspaceErrorEnvelope(tool) {
		return workspaceEditErrorResult("invalid")
	}
	wire, err := json.Marshal(tool)
	if err == nil && len(wire) <= maxWorkspaceFileResponseBytes {
		return result, nil
	}
	value := workspaceFileToolResult{Outcome: "unknown"}
	if body, marshalErr := json.Marshal(tool.StructuredContent); marshalErr == nil {
		_ = json.Unmarshal(body, &value)
	}
	response, _, fallbackErr := finishWorkspaceFileTool(value, workspaceToolError{"response_too_large"})
	return response, fallbackErr
}

func workspaceErrorEnvelope(tool *mcp.CallToolResult) bool {
	body, err := json.Marshal(tool.StructuredContent)
	if err != nil {
		return false
	}
	var value workspaceFileToolResult
	return json.Unmarshal(body, &value) == nil && value.Version == 1 && value.Error != nil
}

func workspaceFileToolName(name string) bool {
	switch name {
	case "stow_workspace_file_read_for_save", "stow_workspace_file_save", "stow_workspace_file_resolve_save", "stow_workspace_file_release_observation":
		return true
	}
	return false
}
