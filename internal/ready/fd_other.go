//go:build !unix && !windows

package ready

import (
	"errors"
	"os"
)

func duplicateReadyFile(int) (*os.File, error) {
	return nil, errors.ErrUnsupported
}
