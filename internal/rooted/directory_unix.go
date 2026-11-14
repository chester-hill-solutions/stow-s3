//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package rooted

import (
	"os"
	"syscall"
)

func openDirectory(root *os.Root, relative string) (*os.File, error) {
	return root.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_DIRECTORY, 0)
}
