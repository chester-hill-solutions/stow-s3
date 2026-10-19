// Package atomicfile writes a file so that a reader sees either the old contents
// or the new ones, and so that a write which reports success survives a crash.
//
// One implementation means one guarantee, and the guarantee includes the errors:
// a caller that renames without syncing the parent directory is not durable until
// the directory entry is synced, and a caller that discards the sync error reports
// success for a write that may be lost.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// Write replaces path atomically and durably, creating the parent if missing.
// File data and permissions are synced before rename, then the parent is synced.
// Every error is returned; a failed directory fence may leave replacement visible.
func Write(path string, data []byte, perm os.FileMode) error {
	return WriteParts(path, perm, data)
}

func WriteParts(path string, perm os.FileMode, parts ...[]byte) error {
	return writeFile(path, perm, func(tmp *os.File) error {
		for _, data := range parts {
			if _, err := tmp.Write(data); err != nil {
				return err
			}
		}
		return nil
	})
}

// WriteStream publishes a prefix and payload through the same durable replacement.
func WriteStream(path string, perm os.FileMode, prefix []byte, body io.Reader) error {
	return writeFile(path, perm, func(tmp *os.File) error {
		if _, err := tmp.Write(prefix); err != nil {
			return err
		}
		_, err := io.Copy(tmp, body)
		return err
	})
}

func writeFile(path string, perm os.FileMode, write func(*os.File) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return wrap(path, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return wrap(path, err)
	}
	tmpName := tmp.Name()
	// Until the rename succeeds the temporary file is litter. Deferred rather
	// than repeated at each failure so a new error path cannot forget it.
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	if err := write(tmp); err != nil {
		_ = tmp.Close()
		return wrap(path, err)
	}
	// The permissions are set before the sync: the mode is part of what has to
	// reach the disk, not a detail to apply afterwards.
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return wrap(path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return wrap(path, err)
	}
	if err := tmp.Close(); err != nil {
		return wrap(path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return wrap(path, err)
	}
	renamed = true
	if err := syncDir(dir); err != nil {
		return wrap(path, err)
	}
	return nil
}

// SyncExisting fences a compatible file without replacing it.
// It requires a regular file with the requested permissions. Callers can replace
// the file using Write if this fence fails.
func SyncExisting(path string, perm os.FileMode) error {
	before, err := os.Lstat(path)
	if err != nil {
		return wrap(path, err)
	}
	if !before.Mode().IsRegular() || (runtime.GOOS != "windows" && before.Mode() != perm) {
		return wrap(path, fmt.Errorf("existing file requires replacement"))
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return wrap(path, err)
	}
	actual, err := file.Stat()
	if err == nil && !os.SameFile(before, actual) {
		err = fmt.Errorf("existing file changed during fence")
	}
	if err == nil {
		err = file.Sync()
	}
	if err := errors.Join(err, file.Close()); err != nil {
		return wrap(path, err)
	}
	return wrapIfError(path, syncDir(filepath.Dir(path)))
}

func wrap(path string, err error) error {
	return fmt.Errorf("atomicfile: write %s: %w", path, err)
}
