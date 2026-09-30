package workspace_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// TestMain skips this package on a host that cannot support a workspace store,
// rather than running forty-odd tests that fail there for one stated reason: a
// lane red for a platform limitation teaches a reader to ignore the lane.
// docs/workspace-contract.md section 6 records Windows as uncertified, and this is
// that fact reaching the gate.
//
// The condition is the package's own capability predicates rather than a GOOS
// check, so a host that gains either runs the package unchanged. The skip is
// printed because the platform job passes -v for that reason.
func TestMain(m *testing.M) {
	if reason := hostCannotSupportAStore(); reason != "" {
		fmt.Printf("SKIP internal/storage/workspace: %s\n", reason)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func hostCannotSupportAStore() string {
	var missing []string
	if !rooted.Supported() {
		missing = append(missing, "no atomic regular-file open, so every rooted read fails closed")
	}
	if !workspace.LockSupported() {
		missing = append(missing, "no advisory file lock, so a capture lock cannot be held")
	}
	return strings.Join(missing, "; ")
}
