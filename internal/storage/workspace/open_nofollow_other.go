//go:build !unix

package workspace

import (
	"fmt"
	"os"
)

// openNoFollow cannot be atomic here: without O_NOFOLLOW a process that makes a link
// and writes the workspace at once has a window the unix build does not have.
func openNoFollow(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("workspace store: open: %w", err)
	}
	return file, nil
}
