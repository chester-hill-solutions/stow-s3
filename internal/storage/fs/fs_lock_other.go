//go:build aix || js || plan9 || solaris || wasip1

package fs

import "errors"

func lockStoreFile(string) (*storeLock, error) {
	return nil, errors.ErrUnsupported
}
