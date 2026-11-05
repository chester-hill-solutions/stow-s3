//go:build unix

package ready

import (
	"os"
	"syscall"
)

func duplicateReadyFile(fd int) (*os.File, error) {
	syscall.ForkLock.RLock()
	duplicate, err := syscall.Dup(fd)
	if err == nil {
		syscall.CloseOnExec(duplicate)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(duplicate), "stow-ready")
	if file == nil {
		syscall.Close(duplicate)
		return nil, os.ErrInvalid
	}
	return file, nil
}
