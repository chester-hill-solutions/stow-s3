package fs

import (
	"fmt"
	"os"
)

const storeLockName = ".stow.lock"

type storeLock struct {
	file *os.File
	path string
}

func acquireStoreLock(lockPath string) (*storeLock, error) {
	lock, err := lockStoreFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("acquire store lock: %w", err)
	}
	return lock, nil
}

func (l *storeLock) release() error {
	if l == nil {
		return nil
	}
	return l.file.Close()
}
