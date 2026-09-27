//go:build windows

package atomicfile

// syncDir is a no-op on Windows, and that is the platform's answer rather than a
// gap in stow's.
//
// os.Open on a directory succeeds, and File.Sync then calls FlushFileBuffers on
// the handle, which Windows refuses for a directory with ERROR_ACCESS_DENIED. The
// failure was not a flake on the windows-latest runner: every workspace test
// failed at manifest write with "sync parent directory ...: Access is denied",
// which meant the workspace backend could not write its manifest on Windows at
// all. A backend that cannot persist is not a degraded backend, it is a broken
// one, and Windows is a first-class release target.
//
// The durability argument differs rather than being skipped. On POSIX, a rename
// is atomic but the *directory entry* recording it is not durable until the
// directory is fsynced, which is why this call exists at all. On Windows the
// rename goes through MoveFileEx, the filesystem journals directory metadata, and
// there is no supported way to flush a directory handle — so the entry is as
// durable as the platform makes it, and asking again only produces an error that
// says nothing about the data.
//
// A caller that genuinely needs a stronger guarantee on Windows has to reach for
// FlushFileBuffers on the file handle before the rename, which is a different
// design and not this function's job. Reporting an error here would be worse than
// silence: every write would fail on a platform where the write is in fact fine.
func syncDir(string) error { return nil }
