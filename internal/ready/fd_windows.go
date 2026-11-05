package ready

import (
	"os"
	"syscall"
)

func duplicateReadyFile(fd int) (*os.File, error) {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return nil, err
	}
	var duplicate syscall.Handle
	if err := syscall.DuplicateHandle(process, syscall.Handle(fd), process, &duplicate, 0, false, syscall.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(duplicate), "stow-ready")
	if file == nil {
		syscall.CloseHandle(duplicate)
		return nil, os.ErrInvalid
	}
	return file, nil
}
