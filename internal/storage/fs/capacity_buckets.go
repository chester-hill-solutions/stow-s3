package fs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
)

func (s *FilesystemStore) createBucketLocked(dir string) error {
	created := time.Now().UTC()
	data, err := json.Marshal(map[string]time.Time{"created_at": created})
	if err != nil {
		return err
	}
	if s.namespace == nil || s.capacityActive {
		return s.publishBucketFiles(dir, data)
	}
	path := filepath.Join(dir, "bucket.json")
	admission, err := s.capacityFileAdmission(path, data)
	if err != nil {
		return err
	}
	return s.namespace.WithAdmission(context.Background(), admission, func() error {
		if err := s.publishBucketFiles(dir, data); err != nil {
			return errors.Join(capacity.ErrUnknown, err)
		}
		if err := syncSaveAncestors(filepath.Join(dir, "objects")); err != nil {
			return errors.Join(capacity.ErrUnknown, err)
		}
		return nil
	})
}

func (s *FilesystemStore) publishBucketFiles(dir string, data []byte) error {
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0755); err != nil {
		return err
	}
	return writeBytesAtomic(filepath.Join(dir, "bucket.json"), data)
}
