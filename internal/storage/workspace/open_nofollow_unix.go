//go:build unix

package workspace

import (
	"fmt"
	"os"
	"syscall"
)

// openNoFollow opens path for reading and refuses a final symbolic link
// atomically. Why it must be atomic rather than checked, why the store reads
// through internal/rooted instead, and why that reader also refuses a linked
// parent, are in docs/workspace-contract.md.
func openNoFollow(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("workspace store: open without following: %w", err)
	}
	return file, nil
}
