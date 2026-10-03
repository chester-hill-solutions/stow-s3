package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// WriteRoot atomically replaces a file using only operations confined to an open root.
// Temp creation, publication, cleanup and directory sync stay confined even if
// an ancestor changes. The caller creates and validates parent directories first.
func WriteRoot(root *os.Root, path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return wrap(path, err)
	}
	name := filepath.Join(dir, ".tmp-"+hex.EncodeToString(nonce[:]))
	tmp, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return wrap(path, err)
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = root.Remove(name)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return wrap(path, err)
	}
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
	if err := root.Rename(name, path); err != nil {
		return wrap(path, err)
	}
	renamed = true
	parentRoot, err := root.OpenRoot(dir)
	if err != nil {
		return wrap(path, err)
	}
	defer parentRoot.Close()
	parent, err := parentRoot.Open(".")
	if err != nil {
		return wrap(path, err)
	}
	return wrapIfError(path, errors.Join(parent.Sync(), parent.Close()))
}

func wrapIfError(path string, err error) error {
	if err != nil {
		return wrap(path, err)
	}
	return nil
}
