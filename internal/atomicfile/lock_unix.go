//go:build android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd

package atomicfile

import (
	"fmt"
	"os"
	"syscall"
)

func lockPath(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("atomicfile: open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("atomicfile: lock %s: %w", path, err)
	}
	return &Lock{release: func() error {
		unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		closeErr := file.Close()
		if unlockErr != nil {
			return fmt.Errorf("atomicfile: unlock %s: %w", path, unlockErr)
		}
		return closeErr
	}}, nil
}
