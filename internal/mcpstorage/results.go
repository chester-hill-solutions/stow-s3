package mcpstorage

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxResponseBytes = 256 << 10

type failure struct {
	Code      string `json:"code"`
	Phase     string `json:"phase"`
	Attempts  int    `json:"attempts"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message"`
}

type workspaceView struct {
	ID                 string `json:"workspace_id"`
	Directory          string `json:"directory"`
	Bucket             string `json:"bucket"`
	MaxCheckpointBytes int64  `json:"max_checkpoint_bytes"`
	MaxCheckpoints     int64  `json:"max_checkpoints"`
}

type entry struct {
	Object       *stow.CheckpointObject       `json:"object,omitempty"`
	Bucket       string                       `json:"bucket,omitempty"`
	ObjectChange *stow.CheckpointObjectChange `json:"object_change,omitempty"`
	BucketChange *stow.CheckpointBucketChange `json:"bucket_change,omitempty"`
	Kind         string                       `json:"kind"`
	File         *stow.CheckpointFile         `json:"file,omitempty"`
	Change       *stow.CheckpointChange       `json:"change,omitempty"`
	Excluded     string                       `json:"excluded,omitempty"`
}

type result struct {
	Version            int                    `json:"version"`
	Outcome            string                 `json:"outcome"`
	Workspace          *workspaceView         `json:"workspace,omitempty"`
	Capture            *stow.CheckpointResult `json:"capture,omitempty"`
	CheckpointID       string                 `json:"checkpoint_id,omitempty"`
	Entries            []entry                `json:"entries,omitempty"`
	NextCursor         string                 `json:"next_cursor,omitempty"`
	Handoff            *stow.Handoff          `json:"handoff,omitempty"`
	Bundle             string                 `json:"bundle,omitempty"`
	OriginCheckpointID string                 `json:"origin_checkpoint_id,omitempty"`
	Error              *failure               `json:"error,omitempty"`
}

func finish(value result, err error) (*mcp.CallToolResult, result, error) {
	value.Version = 1
	if err != nil {
		value.Outcome = "failed"
		value.Error = &failure{Code: "invalid_request", Phase: "validation", Message: err.Error()}
		var captureError *stow.CheckpointError
		if errors.As(err, &captureError) {
			value.Outcome = captureError.Outcome
			value.Error = &failure{Code: captureError.Code, Phase: captureError.Phase, Attempts: captureError.Attempts, Retryable: captureError.Retryable, Message: err.Error()}
		}
		var handoffError *stow.HandoffError
		if errors.As(err, &handoffError) {
			value.Outcome = handoffError.Outcome
			value.Error = &failure{Code: "outcome_unknown", Phase: "commit", Attempts: 1, Message: err.Error()}
		}
	}
	encoded, encodeErr := json.Marshal(value)
	if encodeErr != nil {
		return nil, result{}, encodeErr
	}
	if len(encoded) > maxResponseBytes {
		return finish(result{}, fmt.Errorf("response exceeds byte limit; request a smaller page"))
	}
	return &mcp.CallToolResult{IsError: value.Error != nil, Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}}, value, nil
}
