package mcpstorage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_workspace_file_read_for_save", Description: "Read one bounded workspace file and receive a volatile observation token for a guarded save."}, host.readWorkspaceFileForSave)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_workspace_file_save", Description: "Save bounded text or base64 bytes using the observed token or deliberate replacement and a persistent request key."}, host.saveWorkspaceFile)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_workspace_file_resolve_save", Description: "Resolve the retained result for the original workspace file save request."}, host.resolveWorkspaceFileSave)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_workspace_file_release_observation", Description: "Release a volatile workspace file observation; saved receipts remain retained."}, host.releaseWorkspaceFileObservation)
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
	if utf8.Valid(file.Data) {
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
		if len(*input.Text) > maxObjectPayload {
			return nil, workspaceToolError{"payload_too_large"}
		}
		return []byte(*input.Text), nil
	}
	if len(*input.DataBase64) > base64.StdEncoding.EncodedLen(maxObjectPayload) {
		return nil, workspaceToolError{"payload_too_large"}
	}
	data, err := base64.StdEncoding.Strict().DecodeString(*input.DataBase64)
	if err != nil || len(data) > maxObjectPayload {
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
	return len(call.Params.Arguments) > maxResponseBytes || len(call.Params.Name) > 128
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
	if err == nil && len(wire) <= maxResponseBytes {
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
