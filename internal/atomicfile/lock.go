package atomicfile

import "errors"

// ErrLockUnsupported reports a host with no advisory file locking. Callers get
// an error rather than an unguarded write: the operations needing this are
// read-modify-write, and one without exclusion loses a concurrent writer's
// change without either of them noticing.
var ErrLockUnsupported = errors.New("atomicfile: advisory file locks are unsupported on this host")

// Lock is exclusive advisory ownership of a path, held until Release.
//
// It is here because this package already owns "a file changes the way a caller
// was told it changes", and exclusion is the other half of that promise: a
// durable write two processes interleave is neither atomic in its effect nor
// reportable as a conflict. One implementation means one answer about what
// happens when a lock cannot be taken.
type Lock struct {
	release func() error
}

// Acquire blocks until it owns path, creating it if absent, and fails rather
// than proceeding unguarded where the platform cannot lock at all.
func Acquire(path string) (*Lock, error) { return lockPath(path) }

// Release drops ownership. It is idempotent and deliberately leaves the lock
// file in place: removing a file another process may be waiting to open is how
// a lock ends up held by nobody.
func (l *Lock) Release() error {
	if l == nil || l.release == nil {
		return nil
	}
	err := l.release()
	l.release = nil
	return err
}
