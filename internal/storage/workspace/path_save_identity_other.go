//go:build !darwin && !linux

package workspace

import "os"

func fileSaveNativeIdentity(os.FileInfo) (uint64, uint64, bool) { return 0, 0, false }
