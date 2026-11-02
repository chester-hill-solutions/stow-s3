package workspace

import (
	"errors"
	"io/fs"
	"syscall"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// absence reports whether a failure proves that nothing is there.
//
// Only a confirmed absence may be reported as absence. A path that could not be
// examined has not answered the question, and absence is the guess that loses
// data: a create-only write over an unreadable object replaces bytes that are
// still there. Confirmed absence is a missing path, a path whose component is
// not a directory, or a path naming a link or another non-regular file.
func absence(err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true
	case errors.Is(err, syscall.ENOTDIR):
		return true
	case errors.Is(err, rooted.ErrSymlink), errors.Is(err, rooted.ErrNotRegular):
		return true
	default:
		return false
	}
}

// uncertain reports a failure that leaves an object's state unknown. The cause is kept, so a caller can tell a permission refusal from a device fault.
func uncertain(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(storage.ErrObjectUncertain, err)
}
