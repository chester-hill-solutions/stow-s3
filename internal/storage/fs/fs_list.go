package fs

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"

	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type objectListWalk struct {
	prefix   string
	segments []string
	items    *[]storage.ObjectMeta
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
	if err := s.walkObjects(bucket, opts.Prefix, s.objectsDir(bucket), nil, &items); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return storage.PaginateObjects(items, opts), nil
}

func (s *FilesystemStore) walkObjects(bucket, prefix, dir string, segments []string, items *[]storage.ObjectMeta) error {
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
			if !isListDirectory(entry.Name(), segments) {
				continue
			}
			child := append(append(make([]string, 0, len(segments)+1), segments...), entry.Name())
			if err := s.walkObjects(bucket, prefix, entryPath, child, items); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(entry.Name(), legacyMetaSuffix) {
			continue
		}
		if err := s.appendListedObject(bucket, objectListWalk{prefix: prefix, segments: segments, items: items}, entry.Name(), entryPath); err != nil {
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
	record, err := readObjectRecord(path)
	if err != nil {
		return err
	}
	key, ok := keyFromListedRecord(record, walk.segments, name)
	if !ok || !strings.HasPrefix(key, walk.prefix) {
		return nil
	}
	*walk.items = append(*walk.items, record.meta(bucket, key))
	return nil
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
