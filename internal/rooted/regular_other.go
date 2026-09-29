//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package rooted

import (
	"fmt"
	"os"
)

func openRegularFile(*os.Root, string) (*os.File, error) {
	return nil, fmt.Errorf("rooted: safe regular-file opening is unsupported on this platform")
}
