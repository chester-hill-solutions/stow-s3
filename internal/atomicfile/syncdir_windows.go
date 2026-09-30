//go:build windows

package atomicfile

// syncDir is a no-op on Windows, and that is the platform's answer rather than a
// gap in stow's. os.Open on a directory succeeds and File.Sync then calls
// FlushFileBuffers on the handle, which Windows refuses for a directory, and
// reporting that error made every workspace write fail rather than degrade.
// docs/CODE_STANDARDS.md has the full argument.
func syncDir(string) error { return nil }
