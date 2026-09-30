package stow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type RecoveryReference = workspace.RecoveryReference
type RecoveryHoldRequest = workspace.RecoveryHoldRequest
type RecoveryHold = workspace.RecoveryHold
type RecoveryHoldQuery = workspace.RecoveryHoldQuery
type RecoveryHoldPage = workspace.RecoveryHoldPage

const MaxRecoveryHolds = workspace.MaxRecoveryHolds
const MaxRecoveryReferences = workspace.MaxRecoveryReferences
const MaxRecoveryHoldBytes = workspace.MaxRecoveryHoldBytes
const MaxRecoveryHoldPeakBytes = workspace.MaxRecoveryHoldPeakBytes

var (
	ErrRecoveryHoldConflict = workspace.ErrRecoveryHoldConflict
	ErrRecoveryHoldNotFound = workspace.ErrRecoveryHoldNotFound
	ErrRecoveryHoldFull     = workspace.ErrRecoveryHoldFull
	ErrRecoveryHoldCorrupt  = workspace.ErrRecoveryHoldCorrupt
)

// AddRecoveryHold is host administration over a trusted registry, not a grant from Owner.
func AddRecoveryHold(ctx context.Context, registryDir string, request RecoveryHoldRequest) (RecoveryHold, error) {
	return mutateRecoveryHold(ctx, registryDir, func(registry *workspace.Registry) (RecoveryHold, error) {
		if _, found, err := registry.LookupRecoveryHold(request.ID); err != nil || found {
			if err != nil {
				return RecoveryHold{}, err
			}
			return registry.AdmitRecoveryHold(request, "")
		}
		if err := validateRecoveryReferences(registry.Dir(), request.References); err != nil {
			return RecoveryHold{}, err
		}
		return registry.AdmitRecoveryHold(request, "")
	})
}

func ReleaseRecoveryHold(ctx context.Context, registryDir, id, owner string) (RecoveryHold, error) {
	return mutateRecoveryHold(ctx, registryDir, func(registry *workspace.Registry) (RecoveryHold, error) {
		return registry.FinishRecoveryHold(id, owner, "released")
	})
}

func DiscardRecoveryHold(ctx context.Context, registryDir, id, owner string) (RecoveryHold, error) {
	return mutateRecoveryHold(ctx, registryDir, func(registry *workspace.Registry) (RecoveryHold, error) {
		return registry.FinishRecoveryHold(id, owner, "discarded")
	})
}

func mutateRecoveryHold(ctx context.Context, registryDir string, operation func(*workspace.Registry) (RecoveryHold, error)) (RecoveryHold, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RecoveryHold{}, err
	}
	registry, err := openRegistry(registryDir, "")
	if err != nil {
		return RecoveryHold{}, err
	}
	lock, err := workspace.AcquireMutation(registry.Dir())
	if err != nil {
		return RecoveryHold{}, err
	}
	defer lock.Release()
	if err := ctx.Err(); err != nil {
		return RecoveryHold{}, err
	}
	return operation(registry)
}

func validateRecoveryReferences(registryDir string, references []RecoveryReference) error {
	if len(references) == 0 || len(references) > MaxRecoveryReferences {
		return fmt.Errorf("stow: invalid recovery dependency count")
	}
	for _, reference := range references {
		if _, err := LookupWorkspace(registryDir, reference.WorkspaceID); err != nil {
			return err
		}
		if reference.CheckpointID == "" {
			continue
		}
		manifest, err := LoadCheckpoint(registryDir, reference.CheckpointID)
		if err != nil {
			return err
		}
		if manifest.WorkspaceID != reference.WorkspaceID {
			return fmt.Errorf("stow: recovery checkpoint workspace mismatch")
		}
	}
	return nil
}

func ListRecoveryHolds(ctx context.Context, registryDir string, query RecoveryHoldQuery) (RecoveryHoldPage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RecoveryHoldPage{}, err
	}
	registry, err := openRegistryReadOnly(registryDir, "")
	if err != nil {
		return RecoveryHoldPage{}, err
	}
	page, err := registry.ListRecoveryHolds(query)
	if err != nil {
		return RecoveryHoldPage{}, err
	}
	data, err := json.Marshal(page)
	if err != nil || len(data) > 256<<10 {
		return RecoveryHoldPage{}, fmt.Errorf("stow: recovery hold page exceeds response bound")
	}
	return page, nil
}
