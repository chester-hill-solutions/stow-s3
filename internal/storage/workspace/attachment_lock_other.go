//go:build !unix

package workspace

import "os"

func trySharedLock(_ *os.File) (bool, error) { return false, ErrLockUnsupported }
