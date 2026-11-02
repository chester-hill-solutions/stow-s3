package workspace

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *Store) readRoot() (*rooted.Root, error) {
	root, err := rooted.Open(s.root)
	if err != nil {
		return nil, err
	}
	if s.rootIdentity != nil && !os.SameFile(s.rootIdentity, root.Info()) {
		root.Close()
		return nil, fmt.Errorf("workspace: root identity changed")
	}
	return root, nil
}

func (s *Store) openRead(path string, expected os.FileInfo) (*os.File, error) {
	root, err := s.readRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	relative, err := filepath.Rel(s.root, path)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenRegularFile(relative)
	if err != nil {
		// A host that cannot open safely has not answered the question, and
		// absence is the guess that loses data.
		if errors.Is(err, rooted.ErrUnsupported) {
			return nil, err
		}
		return nil, storage.ErrObjectNotFound
	}
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || expected != nil && !os.SameFile(expected, actual) {
		file.Close()
		return nil, storage.ErrObjectNotFound
	}
	return file, nil
}

func (s *Store) readFile(path string, expected os.FileInfo) ([]byte, error) {
	file, err := s.openRead(path, expected)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, expected.Size()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != expected.Size() {
		return nil, fmt.Errorf("workspace: file changed while reading")
	}
	return data, nil
}

func deriveReadFile(file *os.File, info os.FileInfo) (ManifestEntry, error) {
	defer file.Close()
	hash := md5.New()
	reader := io.TeeReader(io.LimitReader(file, info.Size()+1), hash)
	head := make([]byte, 512)
	n, err := io.ReadFull(reader, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return ManifestEntry{}, err
	}
	copied, err := io.Copy(io.Discard, reader)
	if err != nil {
		return ManifestEntry{}, err
	}
	if int64(n)+copied != info.Size() {
		return ManifestEntry{}, fmt.Errorf("workspace: file changed while deriving metadata")
	}
	contentType := "application/octet-stream"
	if n > 0 {
		contentType = http.DetectContentType(head[:n])
	}
	etag := fmt.Sprintf("\"%s\"", hex.EncodeToString(hash.Sum(nil)))
	return ManifestEntry{Size: info.Size(), ContentType: contentType, Modified: info.ModTime().UTC().Truncate(time.Second), ETag: etag, VersionID: etag}, nil
}

func (s *Store) checkRootIdentity() error {
	if s.rootIdentity == nil {
		return nil
	}
	actual, err := os.Lstat(s.root)
	if err != nil {
		return err
	}
	if !actual.IsDir() || !os.SameFile(s.rootIdentity, actual) {
		return fmt.Errorf("workspace: root identity changed")
	}
	return nil
}
