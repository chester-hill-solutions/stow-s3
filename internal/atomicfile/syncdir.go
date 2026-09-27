//go:build !windows

package atomicfile

import (
	"fmt"
	"os"
)

// syncDir fsyncs a directory so that a rename into it is durable.
//
// Both errors are returned. A directory that cannot be opened, or whose sync
// fails, means the rename may not survive a crash — and reporting success there
// is how "the write was acknowledged" stops meaning anything.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("atomicfile: open parent directory %s: %w", dir, err)
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return fmt.Errorf("atomicfile: sync parent directory %s: %w", dir, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("atomicfile: close parent directory %s: %w", dir, closeErr)
	}
	return nil
}
