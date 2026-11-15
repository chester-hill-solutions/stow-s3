package workspace

import (
	"errors"
	"fmt"
	"path/filepath"
)

// AcquireReadAttachment holds a shared lease on a registered workspace. Callers
// acquire it before looking up the workspace so removal cannot race resolution.
// The lease is separate from session and capture locks: it neither claims writer
// ownership nor prevents checkpoint capture.
func AcquireReadAttachment(registryDir, workspaceID string) (*CaptureLock, error) {
	if registryDir == "" {
		return nil, fmt.Errorf("workspace: a read attachment needs a registry directory")
	}
	return acquireCapture(attachmentLockRoot(registryDir), workspaceID, trySharedLock)
}

func attachmentLockRoot(registryDir string) string {
	return filepath.Join(registryDir, ".stow", "readers")
}

// acquireAttachmentRemoval takes the matching exclusive lease for removal.
func acquireAttachmentRemoval(registryDir, workspaceID string) (*CaptureLock, error) {
	if registryDir == "" {
		return nil, fmt.Errorf("workspace: an attachment removal lock needs a registry directory")
	}
	lock, err := acquireCapture(attachmentLockRoot(registryDir), workspaceID, tryLock)
	if errors.Is(err, ErrCaptureInProgress) {
		return nil, fmt.Errorf("%w: read attachment: %s", ErrWorkspaceInUse, workspaceID)
	}
	return lock, err
}
