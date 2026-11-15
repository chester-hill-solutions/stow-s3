package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrCheckpointInUse = errors.New("workspace: checkpoint is in use")

func AcquireCheckpointReadAttachment(registryDir, checkpointID string) (*CaptureLock, error) {
	return acquireCheckpointAttachment(registryDir, checkpointID, trySharedLock)
}

func AcquireCheckpointRemoval(registryDir, checkpointID string) (*CaptureLock, error) {
	return acquireCheckpointAttachment(registryDir, checkpointID, tryLock)
}

func acquireCheckpointAttachment(registryDir, checkpointID string, take func(*os.File) (bool, error)) (*CaptureLock, error) {
	if registryDir == "" || !validRecoveryCheckpoint(checkpointID) {
		return nil, fmt.Errorf("workspace: a checkpoint attachment needs a registry directory and valid checkpoint id")
	}
	lock, err := acquireCapture(filepath.Join(registryDir, ".stow", "checkpoint-locks"), checkpointID, take)
	if errors.Is(err, ErrCaptureInProgress) {
		return nil, fmt.Errorf("%w: %s", ErrCheckpointInUse, checkpointID)
	}
	return lock, err
}
