package stow

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func workspaceFingerprint(root string) (string, error) {
	hash := sha256.New()
	if _, err := io.WriteString(hash, "stow-workspace-fingerprint\x00v2\x00"); err != nil {
		return "", err
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		return fingerprintWorkspacePath(hash, root, path, entry, walkErr)
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func fingerprintWorkspacePath(hash hash.Hash, root, path string, entry os.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if path == filepath.Join(root, ".stow") {
		return filepath.SkipDir
	}
	if path != root && strings.EqualFold(entry.Name(), ".git") {
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	if entry.IsDir() {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unexpected non-regular file in prepared workspace: %s", path)
	}
	return fingerprintWorkspaceFile(hash, root, path)
}

func fingerprintWorkspaceFile(hash hash.Hash, root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if err := writeFingerprintUint64(hash, uint64(len(rel))); err != nil {
		return err
	}
	if _, err := io.WriteString(hash, rel); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	fileHash := sha256.New()
	size, copyErr := io.Copy(fileHash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := writeFingerprintUint64(hash, uint64(size)); err != nil {
		return err
	}
	_, err = hash.Write(fileHash.Sum(nil))
	return err
}

func writeFingerprintUint64(writer io.Writer, value uint64) error {
	var framed [8]byte
	binary.BigEndian.PutUint64(framed[:], value)
	_, err := writer.Write(framed[:])
	return err
}
