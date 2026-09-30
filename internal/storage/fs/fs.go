package fs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const legacyMetaSuffix = ".stowmeta"

// FilesystemStore persists object records on disk with atomic writes.
// Each object is stored as one JSON record containing its bytes and metadata.
type FilesystemStore struct {
	dataDir        string
	lockPath       string
	lockID         string
	mu             sync.RWMutex
	closeOnce      sync.Once
	saves          saveState
	saveFault      func(string) error
	namespace      *capacity.Namespace
	capacityActive bool
}

var _ storage.Store = (*FilesystemStore)(nil)

// NewFilesystemStore creates a filesystem-backed store rooted at dataDir.
func NewFilesystemStore(dataDir string) (*FilesystemStore, error) {
	return newFilesystemStore(dataDir, nil)
}

func NewFilesystemStoreWithNamespace(dataDir string, namespace *capacity.Namespace) (*FilesystemStore, error) {
	if namespace == nil {
		return nil, capacity.ErrInvalid
	}
	return newFilesystemStore(dataDir, namespace)
}

func newFilesystemStore(dataDir string, namespace *capacity.Namespace) (*FilesystemStore, error) {
	if namespace == nil {
		if _, err := os.Lstat(filepath.Join(dataDir, ".capacity-binding.json")); !os.IsNotExist(err) {
			return nil, capacity.ErrConflict
		}
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "buckets"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, ".multipart"), 0o755); err != nil {
		return nil, err
	}

	lockPath := filepath.Join(dataDir, storeLockName)
	lockID, err := acquireStoreLock(lockPath)
	if err != nil {
		return nil, err
	}
	if err := removeStaleTemps(dataDir); err != nil {
		_ = releaseStoreLock(lockPath, lockID)
		return nil, fmt.Errorf("clean staging files: %w", err)
	}
	warnLegacyLayout(dataDir)
	// Recorded last: the marker asserts stow has finished initializing the
	// directory, so a client that sees it may treat the directory as resettable.
	store := &FilesystemStore{dataDir: dataDir, lockPath: lockPath, lockID: lockID}
	if store.SupportsGuardedWrites() {
		if err := store.initializeSaves(); err != nil {
			_ = releaseStoreLock(lockPath, lockID)
			return nil, err
		}
	}
	writeOwnerMarker(dataDir)
	if namespace != nil {
		if err := store.BindNamespace(namespace); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	return store, nil
}

func removeStaleTemps(dataDir string) error {
	return filepath.WalkDir(dataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".tmp-") {
			return nil
		}
		return os.Remove(path)
	})
}

func warnLegacyLayout(dataDir string) {
	root := filepath.Join(dataDir, "buckets")
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), legacyMetaSuffix) {
			log.Printf("stow: legacy .stowmeta layout detected; legacy data is not migrated")
			return filepath.SkipAll
		}
		return nil
	})
}

func (s *FilesystemStore) bucketDir(bucket string) string {
	return filepath.Join(s.dataDir, "buckets", bucket)
}

func (s *FilesystemStore) objectsDir(bucket string) string {
	return filepath.Join(s.bucketDir(bucket), "objects")
}

func (s *FilesystemStore) usesBoundedPath(bucket, key string) bool {
	legacy := filepath.Join(append([]string{s.objectsDir(bucket)}, objectRelSegments(key)...)...)
	return usesBoundedObjectPath(key) || len(legacy) > maxFilesystemPathLength
}

func (s *FilesystemStore) objectPath(bucket, key string) string {
	legacy := filepath.Join(append([]string{s.objectsDir(bucket)}, objectRelSegments(key)...)...)
	segments := append([]string{s.objectsDir(bucket)}, boundedObjectRelSegments(key)...)
	bounded := filepath.Join(segments...)
	if _, err := os.Stat(bounded); err == nil {
		return bounded
	} else if !os.IsNotExist(err) {
		// A digest-addressed record takes precedence, including when an
		// unexpected filesystem error occurs. Callers will report the error
		// when they access it rather than falling through to stale data.
		return bounded
	}
	if s.usesBoundedPath(bucket, key) {
		// Linux could store keys in the old reversible sharded layout. Continue
		// resolving those records after the bounded layout is introduced.
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
		return bounded
	}
	return legacy
}

func (s *FilesystemStore) isBoundedObjectPath(bucket, path string) bool {
	rel, err := filepath.Rel(s.objectsDir(bucket), path)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) == 3 && parts[0] == boundedPathPrefix && len(parts[1]) == 2 && isHexString(parts[1]) && len(parts[2]) == 64 && isHexString(parts[2])
}

// pruneEmptyShards removes the shard directories a delete leaves behind, up to
// but never including the bucket's objects directory.
//
// os.Remove only succeeds on an empty directory, so the walk stops by itself at
// the first shard that still holds another object. Neither bound is trusted on
// its own: the root is compared by path, and the base name must look like a
// shard, so a future caller with a path outside the objects directory cannot
// walk the loop upward deleting directories it did not create.
func (s *FilesystemStore) pruneEmptyShards(bucket, objPath string) {
	root := s.objectsDir(bucket)
	if rel, err := filepath.Rel(root, objPath); err == nil {
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) == 3 && parts[0] == boundedPathPrefix {
			_ = os.Remove(filepath.Dir(objPath))
			_ = os.Remove(filepath.Dir(filepath.Dir(objPath)))
			return
		}
	}
	for dir := filepath.Dir(objPath); dir != root && filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		base := filepath.Base(dir)
		if base == "." || base == string(filepath.Separator) {
			return
		}
		if !isShardName(base) {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
	}
}

func (s *FilesystemStore) multipartDir(uploadID string) string {
	return filepath.Join(s.dataDir, ".multipart", uploadID)
}

func (s *FilesystemStore) requireBucket(bucket string) error {
	info, err := os.Stat(s.bucketDir(bucket))
	if os.IsNotExist(err) || (err == nil && !info.IsDir()) {
		return storage.ErrBucketNotFound
	}
	return err
}

func (s *FilesystemStore) CreateBucket(_ context.Context, name string) error {
	if err := storage.ValidateBucketName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleSaveLocked(); err != nil {
		return err
	}

	dir := s.bucketDir(name)
	if _, err := os.Stat(dir); err == nil {
		return storage.ErrBucketExists
	}
	if err := os.MkdirAll(s.objectsDir(name), 0o755); err != nil {
		return err
	}
	created := time.Now().UTC()
	return writeJSONAtomic(filepath.Join(dir, "bucket.json"), map[string]time.Time{"created_at": created})
}

func (s *FilesystemStore) DeleteBucket(_ context.Context, name string) error {
	if err := storage.ValidateBucketName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleSaveLocked(); err != nil {
		return err
	}

	dir := s.bucketDir(name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return storage.ErrBucketNotFound
	}
	if hasObjects, err := bucketHasObjects(s.objectsDir(name)); err != nil {
		return err
	} else if hasObjects {
		return storage.ErrBucketNotEmpty
	}
	if hasUploads, err := s.bucketHasMultipartUploads(name); err != nil {
		return err
	} else if hasUploads {
		return storage.ErrBucketNotEmpty
	}
	return os.RemoveAll(dir)
}

func (s *FilesystemStore) HeadBucket(_ context.Context, name string) (*storage.BucketInfo, error) {
	if err := storage.ValidateBucketName(name); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.bucketDir(name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, storage.ErrBucketNotFound
	}
	created, err := readBucketCreated(dir)
	if err != nil {
		return nil, err
	}
	return &storage.BucketInfo{Name: name, CreationDate: created}, nil
}

func (s *FilesystemStore) ListBuckets(_ context.Context) ([]storage.BucketInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	root := filepath.Join(s.dataDir, "buckets")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]storage.BucketInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		created, err := readBucketCreated(filepath.Join(root, e.Name()))
		if err != nil {
			created = time.Now().UTC()
		}
		out = append(out, storage.BucketInfo{Name: e.Name(), CreationDate: created})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *FilesystemStore) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, nil, err
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireBucket(bucket); err != nil {
		return nil, nil, err
	}
	record, err := s.readObject(bucket, key)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, storage.ErrObjectNotFound
		}
		return nil, nil, err
	}
	meta := record.meta(bucket, key)
	return io.NopCloser(bytes.NewReader(record.Data)), &meta, nil
}

func (s *FilesystemStore) HeadObject(_ context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireBucket(bucket); err != nil {
		return nil, err
	}
	record, err := s.readObject(bucket, key)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrObjectNotFound
		}
		return nil, err
	}
	meta := record.meta(bucket, key)
	return &meta, nil
}

func (s *FilesystemStore) DeleteObject(_ context.Context, bucket, key string) error {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return err
	}
	if err := storage.ValidateKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleSaveLocked(); err != nil {
		return err
	}

	if err := s.requireBucket(bucket); err != nil {
		return err
	}
	if err := s.checkRecoveryHoldsLocked(bucket, key); err != nil {
		return err
	}
	objPath := s.objectPath(bucket, key)
	if _, err := os.Stat(objPath); os.IsNotExist(err) {
		return storage.ErrObjectNotFound
	}
	if err := os.Remove(objPath); err != nil {
		return err
	}
	s.pruneEmptyShards(bucket, objPath)
	return nil
}

func (s *FilesystemStore) DeleteObjects(_ context.Context, bucket string, keys []string) ([]string, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}

	if err := s.requireBucket(bucket); err != nil {
		return nil, err
	}

	var deleted []string
	for _, key := range keys {
		if err := storage.ValidateKey(key); err != nil {
			return deleted, err
		}
		if err := s.checkRecoveryHoldsLocked(bucket, key); err != nil {
			return deleted, err
		}
		objPath := s.objectPath(bucket, key)
		// A key that was not there is still confirmed: S3 deletes idempotently
		// and reports it as deleted rather than as an error.
		if _, err := os.Stat(objPath); err == nil {
			if err := os.Remove(objPath); err != nil {
				return deleted, err
			}
			s.pruneEmptyShards(bucket, objPath)
		} else if !os.IsNotExist(err) {
			return deleted, err
		}
		deleted = append(deleted, key)
	}
	return deleted, nil
}

func readBucketCreated(dir string) (time.Time, error) {
	data, err := os.ReadFile(filepath.Join(dir, "bucket.json"))
	if err != nil {
		return time.Time{}, err
	}
	var payload struct {
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return time.Time{}, err
	}
	return payload.CreatedAt.UTC(), nil
}

func (s *FilesystemStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = releaseStoreLock(s.lockPath, s.lockID)
	})
	return err
}

func bucketHasObjects(root string) (bool, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return false, nil
	}
	found := false
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), legacyMetaSuffix) {
			return nil
		}
		found = true
		return filepath.SkipAll
	})
	return found, err
}

func (s *FilesystemStore) bucketHasMultipartUploads(bucket string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(s.dataDir, ".multipart"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := readMultipartManifest(filepath.Join(s.dataDir, ".multipart", entry.Name()))
		if err != nil {
			return false, err
		}
		if manifest.Bucket == bucket {
			return true, nil
		}
	}
	return false, nil
}
