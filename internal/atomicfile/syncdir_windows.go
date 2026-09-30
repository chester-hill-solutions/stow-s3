//go:build windows

package atomicfile

// syncDir is a no-op on Windows, and that is the platform's answer rather than a
// gap in stow's.
//
// os.Open on a directory succeeds and File.Sync then calls FlushFileBuffers on the
// handle, which Windows refuses for a directory with ERROR_ACCESS_DENIED. Every
// workspace test failed at manifest write for that reason, so the workspace backend
// could not persist its manifest on Windows at all, and a backend that cannot
// persist is broken rather than degraded.
//
// The durability argument differs rather than being skipped. On POSIX a rename is
// atomic but the directory entry recording it is not durable until the directory is
// fsynced, which is why this call exists. On Windows the rename goes through
// MoveFileEx, the filesystem journals directory metadata, and there is no supported
// way to flush a directory handle, so the entry is as durable as the platform makes
// it and asking again only produces an error that says nothing about the data.
// A caller needing a stronger guarantee there must FlushFileBuffers the file handle
// before the rename, which is a different design. Reporting an error here would be
// worse than silence: every write would fail where the write is in fact fine.
func syncDir(string) error { return nil }
