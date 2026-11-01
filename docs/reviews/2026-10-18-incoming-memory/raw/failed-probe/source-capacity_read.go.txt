package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
)

func resourceFor(path string, data []byte, class string) capacity.Resource {
	version := sha256.Sum256(data)
	return resourceFromHash(path, version[:], int64(len(data)), class)
}

func resourceFromHash(path string, version []byte, size int64, class string) capacity.Resource {
	name := sha256.Sum256([]byte(filepath.Clean(path)))
	return capacity.Resource{ID: hex.EncodeToString(name[:]), Version: hex.EncodeToString(version), Bytes: size, Class: class}
}

func (s *FilesystemStore) readCapacityResource(path string, buffer *[]byte) (capacity.Resource, error) {
	file, err := os.Open(path)
	if err != nil {
		return capacity.Resource{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return capacity.Resource{}, err
	}
	if info.Size() < 0 {
		return capacity.Resource{}, capacity.ErrUnknown
	}
	class, err := s.validateCapacityFile(path, file)
	if err != nil {
		return capacity.Resource{}, err
	}
	wanted := int(min(info.Size(), (32<<10)-1)) + 1
	if len(*buffer) < wanted {
		*buffer = make([]byte, min(32<<10, max(wanted, 2*len(*buffer))))
	}
	limit := info.Size()
	if limit < math.MaxInt64 {
		limit++
	}
	digest := sha256.New()
	n, err := io.CopyBuffer(digest, io.LimitReader(file, limit), *buffer)
	if err != nil {
		return capacity.Resource{}, err
	}
	if n != info.Size() {
		return capacity.Resource{}, capacity.ErrUnknown
	}
	var sum [sha256.Size]byte
	return resourceFromHash(path, digest.Sum(sum[:0]), n, class), nil
}
