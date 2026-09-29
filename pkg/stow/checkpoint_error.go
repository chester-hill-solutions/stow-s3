package stow

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type CheckpointError struct {
	Code      string `json:"code"`
	Phase     string `json:"phase"`
	Attempts  int    `json:"attempts"`
	Retryable bool   `json:"retryable"`
	Outcome   string `json:"outcome"`
	Err       error  `json:"-"`
}

func (e *CheckpointError) Error() string {
	return fmt.Sprintf("stow: checkpoint %s (%s): %v", e.Code, e.Phase, e.Err)
}
func (e *CheckpointError) Unwrap() error { return e.Err }

func checkpointFailure(code, phase, outcome string, err error) *CheckpointError {
	return &CheckpointError{Code: code, Phase: phase, Attempts: 1, Outcome: outcome, Err: err, Retryable: code == "in_progress"}
}

func classifyCheckpointError(phase string, err error) error {
	if err == nil {
		return nil
	}
	var typed *CheckpointError
	if errors.As(err, &typed) {
		return err
	}
	code := "io_failure"
	switch {
	case errors.Is(err, workspace.ErrCaptureInProgress):
		code = "in_progress"
	case errors.Is(err, workspace.ErrLockUnsupported):
		code = "locking_unavailable"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = "cancelled"
	case errors.Is(err, os.ErrPermission):
		code = "permission_denied"
	}
	return checkpointFailure(code, phase, "not_committed", err)
}
