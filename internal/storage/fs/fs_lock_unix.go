//go:build android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd

package fs

import (
	"os"
	"syscall"
)

func lockStoreFile(path string) (*storeLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &storeLock{file: file, path: path}, nil
}
