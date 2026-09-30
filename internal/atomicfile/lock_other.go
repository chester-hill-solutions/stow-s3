//go:build aix || js || plan9 || solaris || wasip1

package atomicfile

// lockPath refuses rather than returning an unguarded Lock. The callers are
// read-modify-write, so proceeding without exclusion is the one answer that
// cannot later be reported as a conflict.
func lockPath(string) (*Lock, error) { return nil, ErrLockUnsupported }
