package storage

import "errors"

// ErrMutationCommitted means the authoritative local mutation completed, but a
// subsequent step (such as propagation) failed. Callers must account for the
// committed data while still reporting the underlying error.
var ErrMutationCommitted = errors.New("local mutation committed")

func CommittedError(cause error) error {
	if cause == nil {
		return nil
	}
	return errors.Join(ErrMutationCommitted, cause)
}

// QuotaStoreProvider identifies the authoritative data counted by a wrapper's
// runtime quota. Remote listings and an independently bounded cache are excluded.
type QuotaStoreProvider interface{ QuotaStore() Store }
