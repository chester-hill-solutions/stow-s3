package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LockName is the file whose advisory lock is the workspace's liveness signal.
const LockName = "session.lock"

// ErrLockUnsupported is returned on a host with no advisory file lock in the
// standard library, which today means Windows.
//
// The consequence is stated rather than worked around: a workspace on such a
// host is never collectable, because liveness cannot be *established* and this
// package refuses to guess. Failing safe here is not a limitation to engineer
// around — it is the same reason the parent-death watch reports
// ErrUnsupported rather than pretending to work.
var ErrLockUnsupported = errors.New("advisory file locks are unsupported on this host")

// SessionLockSupported reports whether this host can establish workspace liveness.
// Unsupported hosts refuse collection instead of guessing (ADR 0009).
func SessionLockSupported() bool { return lockSupported }

// ErrWorkspaceInUse is returned when a caller asks to reclaim a workspace that a
// live session still holds.
//
// It is a correctness error, not a policy. A workspace is a working directory, so
// collecting one out from under a running agent destroys the artifact it is in
// the middle of producing. Nothing may be deleted on the strength of a guess that
// nobody is using it.
var ErrWorkspaceInUse = errors.New("workspace is in use by a live session")

// Session is a held claim on a workspace, and the only thing that makes
// "is anybody using this?" a question the operating system answers rather than
// one a collector guesses at.
//
// The lock is an advisory file lock, which the kernel releases when the holding
// process dies — including on SIGKILL, and including on an OOM kill. There is
// therefore no stale-lock cleanup to get wrong and no heartbeat to expire
// wrongly: if the lock can be taken, every process that held it is gone.
type Session struct {
	file *os.File
	path string
}

type sessionIdentityPaths struct {
	root, metadata, lock   string
	rootInfo, metadataInfo os.FileInfo
}

func LockSupported() bool { return lockSupported }

// AcquireSession takes the liveness lock for a workspace root.
//
// A workspace may be locked by one session at a time within a process; a second
// acquisition in the same process would succeed at the kernel level only if the
// first released, so callers hold exactly one.
func AcquireSession(root string) (*Session, error) {
	rootInfo, metadataInfo, err := prepareSessionDirectories(root)
	if err != nil {
		return nil, err
	}
	metadataPath := InternalPath(root)
	path := filepath.Join(InternalPath(root), LockName)
	file, err := openSessionLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("workspace: open session lock: %w", err)
	}
	identity := sessionIdentityPaths{root: root, metadata: metadataPath, lock: path, rootInfo: rootInfo, metadataInfo: metadataInfo}
	if err := validateSessionFileIdentity(file, identity); err != nil {
		file.Close()
		return nil, err
	}
	acquired, err := tryLock(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("workspace: lock session: %w", err)
	}
	if !acquired {
		file.Close()
		return nil, fmt.Errorf("%w: %s", ErrWorkspaceInUse, root)
	}
	if err := validateSessionFileIdentity(file, identity); err != nil {
		_ = unlockFile(file)
		_ = file.Close()
		return nil, err
	}
	return &Session{file: file, path: path}, nil
}

func prepareSessionDirectories(root string) (os.FileInfo, os.FileInfo, error) {
	if root == "" {
		return nil, nil, fmt.Errorf("workspace: session root is required")
	}
	rootInfo, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, nil, fmt.Errorf("workspace: prepare session root: %w", err)
		}
		rootInfo, err = os.Lstat(root)
	}
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("workspace: session root must be a real directory")
	}
	metadataPath := InternalPath(root)
	if err := os.Mkdir(metadataPath, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, nil, fmt.Errorf("workspace: prepare session metadata: %w", err)
	}
	metadataInfo, err := os.Lstat(metadataPath)
	if err != nil || !metadataInfo.IsDir() || metadataInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("workspace: session metadata must be a real directory")
	}
	return rootInfo, metadataInfo, nil
}

func ValidateRegisteredSessionRoot(root string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("workspace: registered root is unavailable")
	}
	metadataInfo, err := os.Lstat(InternalPath(root))
	if err != nil || !metadataInfo.IsDir() || metadataInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("workspace: registered metadata directory is unavailable")
	}
	return nil
}

func validateSessionFileIdentity(file *os.File, paths sessionIdentityPaths) error {
	lockInfo, err := file.Stat()
	if err != nil {
		return err
	}
	pathInfo, err := os.Lstat(paths.lock)
	if err != nil {
		return err
	}
	currentRoot, err := os.Lstat(paths.root)
	if err != nil {
		return err
	}
	currentMetadata, err := os.Lstat(paths.metadata)
	if err != nil || !os.SameFile(lockInfo, pathInfo) || !os.SameFile(paths.rootInfo, currentRoot) || !os.SameFile(paths.metadataInfo, currentMetadata) {
		return fmt.Errorf("workspace: session path identity changed")
	}
	return nil
}

// ProbeLiveness reports whether a live session holds a workspace.
//
// It answers by attempting the same lock the holder took: acquiring it proves
// every prior holder is gone, and failing to acquire it proves somebody is here
// now. There is no third answer, which is the point — the alternative is a
// recorded heartbeat that can be stale in exactly the direction that deletes
// somebody's work.
func ProbeLiveness(root string) (live bool, err error) {
	path := filepath.Join(InternalPath(root), LockName)
	// Opened without O_CREATE on purpose: a probe must not leave anything
	// behind. A missing lock file means no session ever took one, so nobody
	// holds the workspace and the answer is simply "not live".
	file, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("workspace: open session lock: %w", err)
	}
	defer file.Close()
	acquired, err := tryLock(file)
	if err != nil {
		return false, err
	}
	if acquired {
		// Take it back off so a later real session is not blocked by the probe.
		if err := unlockFile(file); err != nil {
			return false, fmt.Errorf("workspace: release probe lock: %w", err)
		}
		return false, nil
	}
	return true, nil
}

// Release gives up the claim. It is idempotent, because a caller closing a
// workspace twice must not turn the second close into a failure.
func (s *Session) Release() error {
	if s == nil || s.file == nil {
		return nil
	}
	err := unlockFile(s.file)
	closeErr := s.file.Close()
	s.file = nil
	if err != nil {
		return fmt.Errorf("workspace: release session lock: %w", err)
	}
	return closeErr
}
