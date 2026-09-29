package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type checkpointRequestFlags struct {
	Key     string
	Resolve bool
	Timeout time.Duration
	Options stow.CheckpointOptions
}

type captureCommandResult struct {
	Version int                   `json:"version"`
	Result  stow.CheckpointResult `json:"result"`
	Error   *stow.CheckpointError `json:"error,omitempty"`
}

func checkpointRequestCommand(registry, id string, flags checkpointRequestFlags) error {
	if flags.Key == "" || flags.Timeout <= 0 {
		return fmt.Errorf("capture requests require --request-key and a positive --timeout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), flags.Timeout)
	defer cancel()
	operation := stow.CaptureCheckpoint
	if flags.Resolve {
		operation = stow.ResolveCheckpoint
	}
	saved, err := operation(ctx, registry, id, stow.CheckpointRequest{Key: flags.Key, Options: flags.Options})
	out := captureCommandResult{Version: 1, Result: saved}
	if err != nil {
		var failure *stow.CheckpointError
		if !errors.As(err, &failure) {
			return err
		}
		out.Error = failure
	}
	if writeErr := writeWorkspaceJSON(out); writeErr != nil {
		return writeErr
	}
	return err
}
