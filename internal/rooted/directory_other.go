//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package rooted

import "os"

func openDirectory(_ *os.Root, _ string) (*os.File, error) { return nil, ErrUnsupported }
