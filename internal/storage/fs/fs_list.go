package fs

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type objectListWalk struct {
	ctx      context.Context
	prefix   string
	segments []string
	collect  func(storage.ObjectMeta) error
}

func (s *FilesystemStore) ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.bucketDir(bucket)); os.IsNotExist(err) {
		return nil, storage.ErrBucketNotFound
	}
	candidates := make([]listedObjectCandidate, 0)
	if err := s.walkListCandidates(ctx, s.objectsDir(bucket), nil, opts.Prefix, &candidates); err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].key != candidates[j].key {
			return candidates[i].key < candidates[j].key
		}
		return candidates[i].path < candidates[j].path
	})
	items := make([]storage.ObjectMeta, 0, len(candidates))
	byKey := make(map[string]listedObjectCandidate, len(candidates))
	for _, candidate := range candidates {
		items = append(items, storage.ObjectMeta{Bucket: bucket, Key: candidate.key})
		byKey[candidate.key] = candidate
	}
	result := storage.PaginateObjects(items, opts)
	for i := range result.Objects {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		candidate := byKey[result.Objects[i].Key]
		record := objectRecord{}
		if candidate.record != nil {
			record = *candidate.record
		} else {
			var err error
			record, err = readObjectHeader(candidate.path)
			if err != nil {
				return nil, err
			}
			if record.Key != "" {
				key, ok := keyFromListedRecord(record, candidate.segments, candidate.name)
				if !ok || key != candidate.key {
					return nil, fmt.Errorf("object record key does not match path %q", candidate.path)
				}
			}
		}
		result.Objects[i] = record.meta(bucket, candidate.key)
	}
	return result, nil
}

type listedObjectCandidate struct {
	key      string
	path     string
	name     string
	segments []string
	record   *objectRecord
}

func (s *FilesystemStore) walkListCandidates(ctx context.Context, dir string, segments []string, prefix string, candidates *[]listedObjectCandidate) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		entryPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if !isListDirectory(entry.Name(), segments) {
				continue
			}
			childSegments := append(append([]string(nil), segments...), entry.Name())
			if err := s.walkListCandidates(ctx, entryPath, childSegments, prefix, candidates); err != nil {
				return err
			}
			continue
		}
		if err := s.appendListCandidate(segments, entry.Name(), entryPath, prefix, candidates); err != nil {
			return err
		}
	}
	return nil
}

func (s *FilesystemStore) appendListCandidate(segments []string, name, path, prefix string, candidates *[]listedObjectCandidate) error {
	if strings.HasSuffix(name, legacyMetaSuffix) {
		return nil
	}
	if len(segments) == 2 && segments[0] == boundedPathPrefix {
		return s.appendBoundedListCandidate(segments, name, path, prefix, candidates)
	}
	return appendReversibleListCandidate(segments, name, path, prefix, candidates)
}

func (s *FilesystemStore) appendBoundedListCandidate(segments []string, name, path, prefix string, candidates *[]listedObjectCandidate) error {
	record, err := readObjectHeader(path)
	if err != nil {
		return err
	}
	key, ok := keyFromListedRecord(record, segments, name)
	if !ok || !strings.HasPrefix(key, prefix) {
		return nil
	}
	*candidates = append(*candidates, listedObjectCandidate{
		key: key, path: path, name: name, segments: append([]string(nil), segments...), record: &record,
	})
	return nil
}

func appendReversibleListCandidate(segments []string, name, path, prefix string, candidates *[]listedObjectCandidate) error {
	key, ok := objectKeyFromSegments(append(append([]string(nil), segments...), name))
	if !ok || !strings.HasPrefix(key, prefix) {
		return nil
	}
	*candidates = append(*candidates, listedObjectCandidate{
		key: key, path: path, name: name, segments: append([]string(nil), segments...),
	})
	return nil
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
		if walk.ctx != nil && walk.ctx.Err() != nil {
			return walk.ctx.Err()
		}
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
	if walk.ctx != nil && walk.ctx.Err() != nil {
		return walk.ctx.Err()
	}
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
