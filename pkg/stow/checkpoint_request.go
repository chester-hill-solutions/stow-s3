package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type CheckpointRequest struct {
	Hold    *CheckpointHold   `json:"hold,omitempty"`
	Key     string            `json:"request_key"`
	Options CheckpointOptions `json:"options"`
}

type CheckpointResult struct {
	Checkpoint CheckpointInfo `json:"checkpoint"`
	Replayed   bool           `json:"replayed"`
	Outcome    string         `json:"outcome"`
}

type checkpointReceipt struct {
	Version       int    `json:"version"`
	CheckpointID  string `json:"checkpoint_id"`
	WorkspaceID   string `json:"workspace_id"`
	RequestDigest string `json:"request_digest"`
	OptionsDigest string `json:"options_digest"`
}

// CaptureCheckpoint replays a retained request before admitting a new checkpoint.
// The caller owns writer quiescence; request keys must be random and persisted before calling.
func CaptureCheckpoint(ctx context.Context, registryDir, workspaceID string, request CheckpointRequest) (CheckpointResult, error) {
	return checkpointRequestOperation(ctx, registryDir, workspaceID, request, true)
}

// ResolveCheckpoint verifies and synchronizes an existing result without capturing new state.
// A not_found result does not prove absence before explicit checkpoint cleanup.
func ResolveCheckpoint(ctx context.Context, registryDir, workspaceID string, request CheckpointRequest) (CheckpointResult, error) {
	return checkpointRequestOperation(ctx, registryDir, workspaceID, request, false)
}

func checkpointRequestOperation(ctx context.Context, registryDir, workspaceID string, request CheckpointRequest, create bool) (CheckpointResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	receipt, err := newCheckpointReceipt(workspaceID, request)
	if err != nil {
		return CheckpointResult{}, checkpointFailure("invalid_request", "validate", "not_committed", err)
	}
	registryDir, err = ResolveRegistryDir(registryDir, "")
	if err != nil {
		return CheckpointResult{}, classifyCheckpointError("validate", err)
	}
	if _, err := LookupWorkspace(registryDir, workspaceID); err != nil {
		return CheckpointResult{}, checkpointFailure("invalid_scope", "lookup", "not_committed", err)
	}
	lock, err := workspace.AcquireMutationCapture(registryDir, workspaceID)
	if err != nil {
		return CheckpointResult{}, classifyCheckpointError("lock", err)
	}
	defer lock.Release()
	reference, err := LookupWorkspace(registryDir, workspaceID)
	if err != nil {
		return CheckpointResult{}, checkpointFailure("invalid_scope", "lookup", "not_committed", err)
	}
	result, err := resolveCheckpointReceipt(ctx, registryDir, receipt)
	if err != nil || result.Outcome == "committed" || !create {
		if err == nil && result.Outcome == "committed" {
			err = reconcileCheckpointHold(registryDir, receipt, request.Hold)
		}
		return result, err
	}
	target := captureTarget{
		dir: reference.Dir, registryDir: registryDir, workspaceID: workspaceID,
		maxCheckpointBytes: reference.MaxCheckpointBytes, maxCheckpoints: reference.MaxCheckpoints,
		receipt: &receipt,
	}
	info, err := captureWithRecoveryHold(ctx, target, request)
	if err != nil {
		return CheckpointResult{}, classifyCheckpointError("capture", err)
	}
	return CheckpointResult{Checkpoint: info, Outcome: "committed"}, nil
}

func newCheckpointReceipt(workspaceID string, request CheckpointRequest) (checkpointReceipt, error) {
	if !workspace.ValidWorkspaceID(workspaceID) || len(request.Key) == 0 || len(request.Key) > 256 || strings.TrimSpace(request.Key) != request.Key {
		return checkpointReceipt{}, fmt.Errorf("invalid workspace or request key")
	}
	if request.Options.MaxBytes < 0 || request.Options.MaxFiles < 0 || request.Options.ParentID != "" && !validCheckpointID(request.Options.ParentID) {
		return checkpointReceipt{}, fmt.Errorf("invalid checkpoint options")
	}
	requestHash := sha256.Sum256([]byte("stow-checkpoint-request-v1\x00" + workspaceID + "\x00" + request.Key))
	options, err := checkpointRequestMeaning(request)
	if err != nil {
		return checkpointReceipt{}, err
	}
	optionsHash := sha256.Sum256(options)
	digest := hex.EncodeToString(requestHash[:])
	return checkpointReceipt{Version: 1, CheckpointID: "cp_" + digest[:24], WorkspaceID: workspaceID,
		RequestDigest: digest, OptionsDigest: hex.EncodeToString(optionsHash[:])}, nil
}

func checkpointRequestMeaning(request CheckpointRequest) ([]byte, error) {
	if request.Hold == nil {
		return json.Marshal(request.Options)
	}
	hold := *request.Hold
	if !workspace.ValidWorkspaceID(hold.ID) || !workspace.ValidWorkspaceID(hold.Owner) {
		return nil, fmt.Errorf("invalid capture recovery hold")
	}
	hold.ExpiresAt = hold.ExpiresAt.UTC()
	return json.Marshal(struct {
		Options CheckpointOptions `json:"options"`
		Hold    CheckpointHold    `json:"hold"`
	}{request.Options, hold})
}

func writeCheckpointReceipt(dir string, receipt *checkpointReceipt) error {
	if receipt == nil {
		return nil
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "receipt.json"), data, 0o600)
}

func resolveCheckpointReceipt(ctx context.Context, registryDir string, expected checkpointReceipt) (CheckpointResult, error) {
	dir, err := checkpointDirectory(registryDir, expected.CheckpointID)
	if err != nil {
		return CheckpointResult{}, err
	}
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return CheckpointResult{Outcome: "not_found"}, nil
	} else if err != nil {
		return CheckpointResult{}, checkpointFailure("outcome_unknown", "resolve", "unknown", err)
	}
	receipt, err := readCheckpointReceipt(dir)
	if err != nil {
		return CheckpointResult{}, checkpointFailure("corrupt_checkpoint", "resolve", "unknown", err)
	}
	if receipt != expected {
		return CheckpointResult{}, checkpointFailure("request_conflict", "resolve", "unknown", fmt.Errorf("request identity or options disagree with retained receipt"))
	}
	manifest, err := LoadCheckpoint(registryDir, expected.CheckpointID)
	if err == nil && manifest.WorkspaceID != expected.WorkspaceID {
		err = fmt.Errorf("checkpoint workspace mismatch")
	}
	if err != nil {
		return CheckpointResult{}, checkpointFailure("corrupt_checkpoint", "resolve", "unknown", err)
	}
	info, err := verifyCheckpointReceiptPayload(ctx, dir, manifest)
	if err != nil {
		return CheckpointResult{}, checkpointFailure("corrupt_checkpoint", "resolve", "unknown", err)
	}
	if err := syncCheckpointTree(ctx, dir); err != nil {
		return CheckpointResult{}, checkpointFailure("outcome_unknown", "resolve", "unknown", err)
	}
	if err := syncCheckpointPath(filepath.Dir(dir)); err != nil {
		return CheckpointResult{}, checkpointFailure("outcome_unknown", "resolve", "unknown", err)
	}
	return CheckpointResult{Checkpoint: info, Replayed: true, Outcome: "committed"}, nil
}

func readCheckpointReceipt(dir string) (checkpointReceipt, error) {
	var receipt checkpointReceipt
	data, err := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if err != nil {
		return receipt, err
	}
	if len(data) > 4096 {
		return receipt, fmt.Errorf("checkpoint receipt exceeds size limit")
	}
	err = json.Unmarshal(data, &receipt)
	return receipt, err
}

func verifyCheckpointReceiptPayload(ctx context.Context, dir string, manifest CheckpointManifest) (CheckpointInfo, error) {
	info := CheckpointInfo{Version: manifest.Version, Objects: int64(len(manifest.Objects)), ID: manifest.ID, WorkspaceID: manifest.WorkspaceID, ParentID: manifest.ParentID,
		Created: manifest.Created, Files: int64(len(manifest.Files)), Excluded: manifest.Excluded}
	for name, file := range checkpointPayloadFiles(manifest) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		stat, err := os.Lstat(path)
		if err != nil {
			return CheckpointInfo{}, err
		}
		if !stat.Mode().IsRegular() || uint32(stat.Mode().Perm()) != file.Mode {
			return CheckpointInfo{}, fmt.Errorf("checkpoint file %q has invalid mode", file.Path)
		}
		hash, size, err := digestFileContext(ctx, path)
		if err != nil {
			return CheckpointInfo{}, err
		}
		if hash != file.SHA256 || size != file.Size {
			return CheckpointInfo{}, fmt.Errorf("checkpoint file %q failed integrity validation", file.Path)
		}
		info.Bytes += size
	}
	if err := verifyPortableChecksums(ctx, dir, manifest); err != nil {
		return CheckpointInfo{}, err
	}
	return info, nil
}
