//go:build windows

package atomicfile

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockFileExclusiveLock = 0x00000002
	lockFileReserved      = 0
	lockFileBytes         = ^uint32(0)
)

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx   = kernel32.NewProc("LockFileEx")
	unlockFileEx = kernel32.NewProc("UnlockFileEx")
)

func lockPath(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("atomicfile: open lock %s: %w", path, err)
	}
	// The OVERLAPPED is retained rather than declared inline because
	// UnlockFileEx has to name the same range LockFileEx took, and a stack
	// value that was not kept is a range nobody can unlock.
	var overlapped syscall.Overlapped
	result, _, err := lockFileEx.Call(
		file.Fd(),
		uintptr(lockFileExclusiveLock),
		uintptr(lockFileReserved),
		uintptr(lockFileBytes),
		uintptr(lockFileBytes),
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		_ = file.Close()
		if err == nil {
			err = syscall.EINVAL
		}
		return nil, fmt.Errorf("atomicfile: lock %s: %w", path, err)
	}
	return &Lock{release: func() error {
		unlocked, _, unlockErr := unlockFileEx.Call(
			file.Fd(),
			uintptr(lockFileReserved),
			uintptr(lockFileBytes),
			uintptr(lockFileBytes),
			uintptr(unsafe.Pointer(&overlapped)),
		)
		closeErr := file.Close()
		if unlocked == 0 {
			return unlockErr
		}
		return closeErr
	}}, nil
}
