//go:build !unix

package workspace

import (
	"fmt"
	"os"
)

// openNoFollow cannot be atomic here: these platforms have no O_NOFOLLOW. Every caller
// reaches this through resolveLocked, which refuses a symlinked path, so behaviour
// matches the unix build for anything an ordinary process can construct — but a
// process that can create a link and write the workspace at once has a window the unix
// build does not. See open_nofollow_unix.go.
func openNoFollow(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("workspace store: open: %w", err)
	}
	return file, nil
}
