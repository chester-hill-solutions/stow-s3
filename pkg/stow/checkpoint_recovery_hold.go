package stow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type CheckpointHold struct {
	ID        string    `json:"id"`
	Owner     string    `json:"owner"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

func captureWithRecoveryHold(ctx context.Context, target captureTarget, request CheckpointRequest) (CheckpointInfo, error) {
	if request.Hold == nil {
		return captureCheckpoint(ctx, target, request.Options)
	}
	registry, err := openRegistryReadOnly(target.registryDir, "")
	if err != nil {
		return CheckpointInfo{}, err
	}
	hold, err := admitCheckpointHold(registry, *target.receipt, request.Hold, true)
	if err != nil {
		return CheckpointInfo{}, checkpointFailure("recovery_refused", "admit", "not_committed", err)
	}
	if hold.State != "pending" {
		return CheckpointInfo{}, checkpointFailure("request_conflict", "admit", "not_committed", fmt.Errorf("aborted capture hold cannot be reactivated; use a new request and hold ID"))
	}
	info, err := captureCheckpoint(ctx, target, request.Options)
	if err == nil {
		return info, nil
	}
	var failure *CheckpointError
	if errors.As(err, &failure) && failure.Outcome == "unknown" {
		return info, err
	}
	_, releaseErr := registry.FinishRecoveryHold(hold.ID, hold.Owner, "discarded")
	return info, errors.Join(err, releaseErr)
}

func admitCheckpointHold(registry *workspace.Registry, receipt checkpointReceipt, hold *CheckpointHold, prepared bool) (RecoveryHold, error) {
	request := RecoveryHoldRequest{ID: hold.ID, Owner: hold.Owner, ExpiresAt: hold.ExpiresAt,
		References: []RecoveryReference{{WorkspaceID: receipt.WorkspaceID, CheckpointID: receipt.CheckpointID}}}
	if !prepared {
		if err := validateRecoveryReferences(registry.Dir(), request.References); err != nil {
			return RecoveryHold{}, err
		}
		return registry.AdmitRecoveryHold(request, "")
	}
	return registry.AdmitRecoveryHold(request, receipt.CheckpointID)
}

func reconcileCheckpointHold(registryDir string, receipt checkpointReceipt, hold *CheckpointHold) error {
	if hold == nil {
		return nil
	}
	registry, err := openRegistryReadOnly(registryDir, "")
	if err != nil {
		return err
	}
	_, err = admitCheckpointHold(registry, receipt, hold, false)
	if err != nil {
		return checkpointFailure("recovery_refused", "resolve", "unknown", err)
	}
	return nil
}
