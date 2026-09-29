package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CaptureLock excludes capture, reconciliation and deletion for one workspace.
type CaptureLock struct {
	file *os.File
	held bool
}

// Held reports whether exclusion is held.
func (c *CaptureLock) Held() bool { return c != nil && c.held }

var ErrCaptureInProgress = errors.New("another capture of this workspace is in progress")

// AcquireCapture fails closed when exclusion is unavailable.
func AcquireCapture(registryDir, workspaceID string) (*CaptureLock, error) {
	return acquireCapture(registryDir, workspaceID, tryLock)
}

func acquireCapture(registryDir, workspaceID string, take func(*os.File) (bool, error)) (*CaptureLock, error) {
	if registryDir == "" || !ValidWorkspaceID(workspaceID) {
		return nil, fmt.Errorf("workspace: a capture lock needs a registry directory and a workspace id")
	}
	dir := filepath.Join(registryDir, captureLockDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: prepare capture lock: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, captureLockName(workspaceID)), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("workspace: open capture lock: %w", err)
	}
	acquired, err := take(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("workspace: acquire capture lock: %w", err)
	}
	if !acquired {
		_ = file.Close()
		return nil, fmt.Errorf("%w: %s", ErrCaptureInProgress, workspaceID)
	}
	return &CaptureLock{file: file, held: true}, nil
}

// Release drops the capture lock. It is idempotent, and it deliberately leaves
// the lock file in place: removing a file another process may be waiting to open
// is how a lock ends up held by nobody.
func (c *CaptureLock) Release() error {
	if c == nil || c.file == nil {
		return nil
	}
	err := unlockFile(c.file)
	closeErr := c.file.Close()
	c.file = nil
	c.held = false
	if err != nil {
		return fmt.Errorf("workspace: release capture lock: %w", err)
	}
	return closeErr
}

const captureLockDir = "capture-locks"

func captureLockName(workspaceID string) string {
	return workspaceID + ".lock"
}
