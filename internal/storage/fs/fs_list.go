package fs

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type objectListWalk struct {
	prefix   string
	segments []string
	collect  func(storage.ObjectMeta) error
}

func (s *FilesystemStore) ListObjectsV2(_ context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.bucketDir(bucket)); os.IsNotExist(err) {
		return nil, storage.ErrBucketNotFound
	}
	items := make([]storage.ObjectMeta, 0)
	walk := objectListWalk{prefix: opts.Prefix, collect: func(meta storage.ObjectMeta) error {
		items = append(items, meta)
		return nil
	}}
	if err := s.walkObjects(bucket, s.objectsDir(bucket), walk); err != nil {
		return nil, err
	}
	return storage.PaginateObjects(items, opts), nil
}

func (s *FilesystemStore) walkObjects(bucket, dir string, walk objectListWalk) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if !isListDirectory(entry.Name(), walk.segments) {
				continue
			}
			child := walk
			child.segments = append(append(make([]string, 0, len(walk.segments)+1), walk.segments...), entry.Name())
			if err := s.walkObjects(bucket, entryPath, child); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(entry.Name(), legacyMetaSuffix) {
			continue
		}
		if err := s.appendListedObject(bucket, walk, entry.Name(), entryPath); err != nil {
			return err
		}
	}
	return nil
}

func isListDirectory(name string, segments []string) bool {
	if isShardName(name) {
		return true
	}
	if len(segments) == 0 {
		return name == boundedPathPrefix
	}
	return len(segments) == 1 && segments[0] == boundedPathPrefix && len(name) == 2 && isHexString(name)
}

func (s *FilesystemStore) appendListedObject(bucket string, walk objectListWalk, name, path string) error {
	record, err := readObjectHeader(path)
	if err != nil {
		return err
	}
	key, ok := keyFromListedRecord(record, walk.segments, name)
	if !ok || !strings.HasPrefix(key, walk.prefix) {
		return nil
	}
	return walk.collect(record.meta(bucket, key))
}

func keyFromListedRecord(record objectRecord, segments []string, name string) (string, bool) {
	if record.Key == "" {
		return objectKeyFromSegments(append(segments, name))
	}
	if storage.ValidateKey(record.Key) != nil {
		return "", false
	}
	want := boundedObjectRelSegments(record.Key)
	got := append(append([]string(nil), segments...), name)
	return record.Key, sameSegments(got, want)
}

func isHexString(value string) bool {
	if value == "" {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sameSegments(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (s *FilesystemStore) ScanObjectUsage(ctx context.Context, bucket string) (storage.ObjectUsage, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return storage.ObjectUsage{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.requireBucket(bucket); err != nil {
		return storage.ObjectUsage{}, err
	}
	var usage storage.ObjectUsage
	walk := objectListWalk{collect: func(meta storage.ObjectMeta) error {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if meta.Size < 0 || usage.Bytes > math.MaxInt64-meta.Size {
			return fmt.Errorf("invalid object usage")
		}
		usage.Bytes += meta.Size
		usage.Objects++
		return nil
	}}
	err := s.walkObjects(bucket, s.objectsDir(bucket), walk)
	return usage, err
}
