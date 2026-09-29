//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package rooted

import (
	"fmt"
	"os"
	"syscall"
)

func openRegularFile(root *os.Root, relative string) (*os.File, error) {
	file, err := root.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("rooted: opened file is not regular")
	}
	return file, nil
}
