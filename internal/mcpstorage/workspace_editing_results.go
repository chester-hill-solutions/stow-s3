package mcpstorage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type workspaceFileObjectResult struct {
	Path         string            `json:"path"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	LastModified time.Time         `json:"last_modified"`
}

type workspaceFileToolResult struct {
	Version          int                        `json:"version"`
	Outcome          string                     `json:"outcome"`
	Available        bool                       `json:"available"`
	Path             string                     `json:"path,omitempty"`
	Absent           bool                       `json:"absent"`
	DataBase64       *string                    `json:"data_base64,omitempty"`
	Text             *string                    `json:"text,omitempty"`
	SHA256           string                     `json:"sha256,omitempty"`
	Mode             *uint32                    `json:"mode,omitempty"`
	Modified         time.Time                  `json:"modified,omitempty"`
	Generation       string                     `json:"generation,omitempty"`
	ObservationToken string                     `json:"observation_token,omitempty"`
	ExpiresAt        time.Time                  `json:"expires_at,omitempty"`
	Replayed         bool                       `json:"replayed,omitempty"`
	Committed        bool                       `json:"committed,omitempty"`
	Released         bool                       `json:"released,omitempty"`
	Object           *workspaceFileObjectResult `json:"object,omitempty"`
	Error            *objectFailure             `json:"error,omitempty"`
}

type workspaceToolError struct{ code string }

func (e workspaceToolError) Error() string { return e.code }

func workspaceFileSaveResult(saved stow.WorkspaceFileSaveResult) workspaceFileToolResult {
	result := workspaceFileToolResult{Outcome: string(saved.Outcome), Replayed: saved.Replayed, Generation: saved.Generation}
	if saved.Outcome == stow.SaveCommitted {
		result.Available = true
		result.Object = &workspaceFileObjectResult{Path: saved.Object.Key, Size: saved.Object.Size, ETag: saved.Object.ETag,
			ContentType: saved.Object.ContentType, Metadata: cloneWorkspaceFileMetadata(saved.Object.Metadata), LastModified: saved.Object.LastModified}
	}
	return result
}

func cloneWorkspaceFileMetadata(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func finishWorkspaceFileTool(value workspaceFileToolResult, err error) (*mcp.CallToolResult, workspaceFileToolResult, error) {
	value.Version = 1
	if value.Outcome == "" {
		value.Outcome = "unknown"
	}
	if err != nil {
		value.Error = workspaceFileFailure(err)
		value.Committed = value.Committed || errors.Is(err, stow.ErrMutationCommitted)
	}
	response, encodeErr := encodeWorkspaceFileTool(value)
	if encodeErr == nil && len(mustMarshalWorkspaceResult(response)) <= maxResponseBytes {
		return response, value, nil
	}
	if value.Text != nil {
		value.Text = nil
		response, encodeErr = encodeWorkspaceFileTool(value)
		if encodeErr == nil && len(mustMarshalWorkspaceResult(response)) <= maxResponseBytes {
			return response, value, nil
		}
	}
	value = boundedWorkspaceFailure(value)
	response, encodeErr = encodeWorkspaceFileTool(value)
	return response, value, encodeErr
}

func encodeWorkspaceFileTool(value workspaceFileToolResult) (*mcp.CallToolResult, error) {
	wire, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{IsError: value.Error != nil, StructuredContent: json.RawMessage(wire),
		Content: []mcp.Content{&mcp.TextContent{Text: string(wire)}}}, nil
}

func mustMarshalWorkspaceResult(response *mcp.CallToolResult) []byte {
	data, _ := json.Marshal(response)
	return data
}

func boundedWorkspaceFailure(original workspaceFileToolResult) workspaceFileToolResult {
	return workspaceFileToolResult{Version: 1, Outcome: original.Outcome, Replayed: original.Replayed,
		Committed: original.Committed, Generation: original.Generation, Error: &objectFailure{Code: "response_too_large", Message: "workspace file response exceeds its limit"}}
}

func workspaceFileFailure(err error) *objectFailure {
	var custom workspaceToolError
	if errors.As(err, &custom) {
		return &objectFailure{Code: custom.code, Message: "workspace file operation " + custom.code}
	}
	if denied := objectFailureOf(err); denied.Code != "unavailable" {
		return denied
	}
	for _, item := range []struct {
		err  error
		code string
	}{
		{stow.ErrWorkspaceEditorClosed, "closed"}, {stow.ErrWorkspaceObservationInvalid, "invalid_observation"},
		{stow.ErrWorkspaceReadTooLarge, "payload_too_large"}, {stow.ErrWorkspaceEditorUnavailable, "unavailable"},
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "canceled"},
	} {
		if errors.Is(err, item.err) {
			return &objectFailure{Code: item.code, Message: "workspace file operation " + item.code}
		}
	}
	return &objectFailure{Code: "unavailable", Message: "workspace file operation unavailable"}
}
