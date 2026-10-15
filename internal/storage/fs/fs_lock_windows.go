//go:build windows

package fs

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	lockFileFailImmediately = 0x00000001
	lockFileExclusiveLock   = 0x00000002
	lockFileReserved        = 0
	lockFileBytes           = ^uint32(0)
)

var (
	kernel32   = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx = kernel32.NewProc("LockFileEx")
)

func lockStoreFile(path string) (*storeLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped syscall.Overlapped
	result, _, callErr := lockFileEx.Call(
		file.Fd(),
		uintptr(lockFileExclusiveLock|lockFileFailImmediately),
		uintptr(lockFileReserved),
		uintptr(lockFileBytes),
		uintptr(lockFileBytes),
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		_ = file.Close()
		if callErr == nil {
			callErr = syscall.EINVAL
		}
		return nil, callErr
	}
	return &storeLock{file: file, path: path}, nil
}
