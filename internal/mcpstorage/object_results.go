package mcpstorage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type objectFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type objectView struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	LastModified time.Time         `json:"last_modified"`
	DataBase64   string            `json:"data_base64,omitempty"`
}
type objectCapabilities struct {
	Backend               string `json:"backend"`
	Persistent            bool   `json:"persistent"`
	GuardedSaves          bool   `json:"guarded_saves"`
	DurableSaveRequests   bool   `json:"durable_save_requests"`
	ReadOnly              bool   `json:"read_only"`
	MaxObjectBytes        int    `json:"max_object_bytes"`
	MaxObservations       int    `json:"max_observations"`
	ObservationTTLSeconds int    `json:"observation_ttl_seconds"`
	MaxResponseBytes      int    `json:"max_response_bytes"`
	MaxToolArgumentsBytes int    `json:"max_tool_arguments_bytes"`
	MaxConcurrentCalls    int    `json:"max_concurrent_calls"`
}
type objectResult struct {
	Version          int                 `json:"version"`
	Outcome          string              `json:"outcome"`
	Available        bool                `json:"available"`
	Object           *objectView         `json:"object,omitempty"`
	ObservationToken string              `json:"observation_token,omitempty"`
	Absent           bool                `json:"absent"`
	Replayed         bool                `json:"replayed"`
	Released         bool                `json:"released,omitempty"`
	Capabilities     *objectCapabilities `json:"capabilities,omitempty"`
	Error            *objectFailure      `json:"error,omitempty"`
}

type objectError struct{ code string }

func (e objectError) Error() string { return e.code }

func objectMetadata(object stow.Object, body bool) *objectView {
	view := &objectView{Key: object.Key, Size: object.Size, ETag: object.ETag, ContentType: object.ContentType, Metadata: object.Metadata, LastModified: object.LastModified}
	if body {
		view.DataBase64 = base64.StdEncoding.EncodeToString(object.Data)
	}
	return view
}

func finishObject(value objectResult, err error) (*mcp.CallToolResult, objectResult, error) {
	value.Version = 1
	if value.Outcome == "" {
		value.Outcome = "unknown"
	}
	if err != nil {
		value.Error = objectFailureOf(err)
	}
	response, encodeErr := encodeObjectResult(value)
	if encodeErr != nil {
		return minimalObjectResult(value, "response_encoding")
	}
	wire, encodeErr := json.Marshal(response)
	if encodeErr != nil {
		return minimalObjectResult(value, "response_encoding")
	}
	if len(wire) > maxResponseBytes {
		return minimalObjectResult(value, "response_too_large")
	}
	return response, value, encodeErr
}

func minimalObjectResult(original objectResult, code string) (*mcp.CallToolResult, objectResult, error) {
	if original.Outcome == "" {
		original.Outcome = "unknown"
	}
	value := objectResult{Version: 1, Outcome: original.Outcome, Replayed: original.Replayed, Error: objectFailureOf(objectError{code})}
	response, err := encodeObjectResult(value)
	return response, value, err
}

func encodeObjectResult(value objectResult) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{IsError: value.Error != nil, StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
}

func objectFailureOf(err error) *objectFailure {
	var custom objectError
	if errors.As(err, &custom) {
		return &objectFailure{Code: custom.code, Message: "object request " + custom.code}
	}
	var denied *stow.ErrNotAuthorized
	if errors.As(err, &denied) {
		return &objectFailure{Code: "denied", Message: "object operation denied"}
	}
	for _, item := range []struct {
		err  error
		code string
	}{
		{stow.ErrSaveConflict, "conflict"}, {stow.ErrSaveRequestConflict, "request_conflict"},
		{stow.ErrSaveRequestNotFound, "not_found"}, {stow.ErrSaveRequestNotCommitted, "not_committed"},
		{stow.ErrSaveRequestFull, "retention_full"}, {stow.ErrQuotaExceeded, "quota_exceeded"},
		{stow.ErrInvalidSaveCondition, "invalid"}, {stow.ErrInvalidSaveRequest, "invalid"},
		{stow.ErrInvalidKey, "invalid"}, {stow.ErrObjectNotFound, "not_found"},
		{stow.ErrClosed, "closed"}, {context.Canceled, "canceled"}, {context.DeadlineExceeded, "canceled"},
		{stow.ErrGuardedSavesUnsupported, "unsupported"}, {stow.ErrSaveRequestsUnsupported, "unsupported"},
	} {
		if errors.Is(err, item.err) {
			return &objectFailure{Code: item.code, Message: "object operation " + item.code}
		}
	}
	return &objectFailure{Code: "unavailable", Message: "object operation unavailable"}
}
