//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package rooted

import "os"

// Without O_NOFOLLOW there is no atomic way to refuse a link and open the file.
const safeRegularOpenSupported = false

// openRegularFile refuses rather than opening a path it cannot prove is not a
// link, which is what lets Supported() mean anything.
func openRegularFile(*os.Root, string) (*os.File, error) {
	return nil, ErrUnsupported
}
