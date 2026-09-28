package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CaptureLock serializes checkpoint captures of one workspace across processes.
//
// It is deliberately not the session lock, because it answers a different
// question. The session lock answers "is anybody using this workspace?", which is
// what stops a collector from deleting the bytes somebody is producing, and it is
// held exclusively by a live handle for as long as that handle exists. This lock
// answers "are two captures of the same workspace racing?", which is what stops
// both of them from passing the same retention check and leaving the cap
// exceeded. Neither question can answer the other, and sharing a file between them
// would mean a capture could not run while a session is live — the thing this
// exists to allow.
type CaptureLock struct {
	file *os.File
	held bool
}

// Held reports whether the lock was actually taken. It is false on a host with no
// advisory lock, where captures are not serialized at all.
func (c *CaptureLock) Held() bool { return c != nil && c.held }

// ErrCaptureInProgress reports that another capture of the same workspace is
// already running.
//
// It is a refusal a caller can act on rather than a failure: nothing is wrong, and
// the same call a moment later succeeds. Waiting inside this function instead
// would be worse than refusing, because a capture lock is an advisory file lock
// and two acquisitions in one process are two independent claims — a blocking
// wait would wait on a lock this process is itself going to hold, and hang.
var ErrCaptureInProgress = errors.New("another capture of this workspace is in progress")

// AcquireCapture takes the capture lock for one workspace, or refuses if one is
// already in progress.
//
// A checkpoint's retention caps are accounted from the published checkpoints, so
// two captures that both read the count before either publishes would both pass.
// Serializing them is what makes the cap exact rather than approximate, and the
// loser of the race is refused before it reaches the retention check rather than
// after it.
//
// Where the host has no advisory lock, the lock is a no-op and Held reports false.
// The consequence is stated rather than worked around: concurrent captures are
// then not serialized, and the cap can be exceeded by one per capture in flight.
// The cap bounds retained disk and is not a safety property — the workspace bytes
// are never at risk, because a capture only reads the workspace and writes into
// the checkpoint store.
func AcquireCapture(registryDir, workspaceID string) (*CaptureLock, error) {
	if registryDir == "" || workspaceID == "" {
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
	acquired, err := tryLock(file)
	if err != nil {
		_ = file.Close()
		// A host that cannot lock cannot serialize, and saying so is better than
		// holding a lock that does not lock.
		return &CaptureLock{}, nil
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

// captureLockName names one workspace's lock. The workspace ID is a generated
// identifier, so it needs no further validation, but it becomes a filename and
// this is the one place that decides so.
func captureLockName(workspaceID string) string {
	return workspaceID + ".lock"
}
