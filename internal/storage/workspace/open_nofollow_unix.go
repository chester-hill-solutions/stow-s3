//go:build unix

package workspace

import (
	"fmt"
	"os"
	"syscall"
)

// openNoFollow opens path for reading and refuses it if the final component is a
// symbolic link, atomically.
//
// The atomicity is why this is a flag and not a check. resolveLocked has already
// established that nothing at path is a symlink, and a plain os.Open between that
// observation and the open is a window: whatever can write the workspace can replace
// the file with a link in between, and the open then follows it. O_NOFOLLOW makes the
// check and the open one operation, in the kernel.
//
// A parent directory that is a symlink is deliberately still followed. Adopted
// projects legitimately contain those — a dotfile repository is mostly symlinks — and
// refusing them would make the product unusable on a real one. The boundary is the one
// the checkpoint path already draws: the object is a regular file, or it is not part of
// the workspace.
func openNoFollow(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("workspace store: open without following: %w", err)
	}
	return file, nil
}
