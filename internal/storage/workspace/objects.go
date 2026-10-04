package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *Store) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, err
	}
	if !s.bucketExists(bucket) {
		return nil, storage.ErrBucketNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putLocked(ctx, bucket, key, body, opts)
}

// putLocked is PutObject's body, split out so that multipart completion, which
// already holds the lock, can assemble an object without deadlocking on a
// non-reentrant mutex.
func (s *Store) putLocked(_ context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	// peekLocked, not peek: the lock is already held, and peek would take it a
	// second time whenever the key has no manifest entry or a stale one - which is
	// every write over a file the host has touched, and every adoption.
	existing, err := s.peekLocked(bucket, key)
	if err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		return nil, err
	}
	if err := storage.CheckWritePreconditions(opts, existing); err != nil {
		return nil, err
	}

	data, err := storage.BytesOf(body)
	if err != nil {
		return nil, err
	}
	if err := storage.VerifyChecksum(opts, data); err != nil {
		return nil, err
	}

	version, err := storage.NewRecordVersion()
	if err != nil {
		return nil, err
	}

	absPath, form := s.pathFor(bucket, key)
	if err := s.writeAtomic(absPath, data); err != nil {
		return nil, fmt.Errorf("workspace store: write %s: %w", key, err)
	}
	written, err := s.openRead(absPath, nil)
	if err != nil {
		return nil, fmt.Errorf("workspace store: open written %s: %w", key, err)
	}
	info, err := written.Stat()
	written.Close()
	if err != nil {
		return nil, fmt.Errorf("workspace store: stat written %s: %w", key, err)
	}

	entry := ManifestEntry{
		Form:              form,
		Size:              int64(len(data)),
		ETag:              storage.ETagForBytes(data),
		VersionID:         version,
		ContentType:       opts.ContentType,
		Metadata:          storage.CloneMetadata(opts.Metadata),
		ChecksumAlgorithm: storage.NormalizeChecksumAlgorithm(opts.ChecksumAlgorithm),
		ChecksumValue:     opts.ChecksumValue,
		Modified:          info.ModTime().UTC().Truncate(time.Second),
	}
	if form == FormEscaped {
		entry.Digest = Digest(key)
	} else {
		s.folded[foldKey(bucket, key)] = key
	}
	s.objectIndex.setEntry(bucket, key, entry)
	if err := s.objectIndex.save(); err != nil {
		return s.metaFromEntry(bucket, key, entry), storage.CommittedError(err)
	}
	return s.metaFromEntry(bucket, key, entry), nil
}

// GetObject returns an object's bytes. The file may be one stow wrote or one
// the host wrote; the second case is the reason this backend exists.
func (s *Store) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	if err := s.checkOpen(); err != nil {
		return nil, nil, err
	}
	if !s.bucketExists(bucket) {
		return nil, nil, storage.ErrBucketNotFound
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	absPath, info, entry, err := s.resolve(bucket, key)
	if err != nil {
		return nil, nil, err
	}
	file, err := s.openRead(absPath, info)
	if err != nil {
		return nil, nil, storage.ErrObjectNotFound
	}
	return file, s.metaFromEntry(bucket, key, entry, info), nil
}

// HeadObject returns an object's metadata without its bytes, deriving it from
// the file when stow has no manifest entry for it.
func (s *Store) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	if !s.bucketExists(bucket) {
		return nil, storage.ErrBucketNotFound
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	_, info, entry, err := s.resolve(bucket, key)
	if err != nil {
		return nil, err
	}
	return s.metaFromEntry(bucket, key, entry, info), nil
}

// DeleteObject removes an object's file and forgets it.
func (s *Store) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := s.checkOpen(); err != nil {
		return err
	}
	if !s.bucketExists(bucket) {
		return storage.ErrBucketNotFound
	}
	if err := storage.ValidateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	absPath, err := s.locate(bucket, key)
	if err != nil {
		return err
	}
	if err := s.removeConfined(absPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return storage.ErrObjectNotFound
		}
		return fmt.Errorf("workspace store: delete %s: %w", key, err)
	}
	s.pruneEmptyParents(bucket, absPath)
	s.forget(bucket, key)
	return s.objectIndex.save()
}

// DeleteObjects removes several keys and returns the ones that were not
// removed, which is the S3 delete-many shape.
func (s *Store) DeleteObjects(ctx context.Context, bucket string, keys []string) ([]string, error) {
	if !s.bucketExists(bucket) {
		return nil, storage.ErrBucketNotFound
	}
	// The returned slice is the keys this call confirmed deleted. A key that was
	// not there counts: S3 deletes idempotently and reports a missing key as
	// deleted rather than as an error, so it belongs in the slice the S3 handler
	// emits as <Deleted>. Only a key whose delete failed is left out.
	var deleted []string
	for _, key := range keys {
		if err := s.DeleteObject(ctx, bucket, key); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
			return deleted, err
		}
		deleted = append(deleted, key)
	}
	return deleted, nil
}

// CopyObject writes an existing object to a new key, as a real file at the new
// key's natural path where that is possible.
func (s *Store) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (*storage.ObjectMeta, error) {
	return s.CopyObjectCond(ctx, storage.CopyRequest{
		SourceBucket: srcBucket,
		SourceKey:    srcKey,
		DestBucket:   dstBucket,
		DestKey:      dstKey,
	})
}

// CopyObjectCond captures the source and publishes the destination under one lock, and
// evaluates the conditions against the version it captured. The read and the write
// share one lock, so a source overwritten in between cannot be copied as the bytes of
// one version with the metadata of another.
func (s *Store) CopyObjectCond(ctx context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	if !s.bucketExists(req.DestBucket) {
		return nil, storage.ErrBucketNotFound
	}
	if err := storage.ValidateCopyRequest(req); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// resolveLocked, not resolve: the lock is already held, and resolve would
	// take it a second time on this goroutine and hang along with every other
	// goroutine behind it. See the note on resolveLocked.
	srcPath, info, entry, err := s.resolveLocked(req.SourceBucket, req.SourceKey)
	if err != nil {
		return nil, err
	}
	sourceMeta := s.metaFromEntry(req.SourceBucket, req.SourceKey, entry, info)
	if err := storage.CheckCopySourceConditions(req.Options, sourceMeta); err != nil {
		return nil, err
	}
	data, err := s.readFile(srcPath, info)
	if err != nil {
		return nil, storage.ErrObjectNotFound
	}
	return s.putLocked(ctx, req.DestBucket, req.DestKey, bytes.NewReader(data), storage.PutOptions{
		ContentType:       sourceMeta.ContentType,
		Metadata:          sourceMeta.Metadata,
		ChecksumAlgorithm: sourceMeta.ChecksumAlgorithm,
		ChecksumValue:     sourceMeta.ChecksumValue,
	})
}

// ListObjectsV2 lists a bucket's objects: every file in its tree, plus the
// escaped keys the manifest records.
func (s *Store) ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	if !s.bucketExists(bucket) {
		return nil, storage.ErrBucketNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	all, err := s.listAll(bucket)
	if err != nil {
		return nil, err
	}
	filtered := make([]storage.ObjectMeta, 0, len(all))
	for _, meta := range all {
		if opts.Prefix == "" || strings.HasPrefix(meta.Key, opts.Prefix) {
			filtered = append(filtered, meta)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Key < filtered[j].Key })
	return storage.PaginateObjects(filtered, opts), nil
}

// listAll returns every object in a bucket, from the tree and from the manifest
// for escaped keys.
func (s *Store) listAll(bucket string) ([]storage.ObjectMeta, error) {
	byKey := map[string]storage.ObjectMeta{}
	root := s.bucketDir(bucket)
	source, err := s.readRoot()
	if err != nil {
		return nil, err
	}
	defer source.Close()
	start, err := filepath.Rel(s.root, root)
	if err != nil {
		return nil, err
	}

	collect := naturalObjectListing{store: s, bucket: bucket, start: start, source: source, byKey: byKey}
	walkErr := fs.WalkDir(source.FS(), filepath.ToSlash(start), collect.visit)
	if walkErr != nil {
		return nil, fmt.Errorf("workspace store: list %s: %w", bucket, walkErr)
	}
	if err := source.Check(); err != nil {
		return nil, err
	}

	for key, meta := range s.escapedObjects(bucket, byKey) {
		byKey[key] = meta
	}

	out := make([]storage.ObjectMeta, 0, len(byKey))
	for _, meta := range byKey {
		out = append(out, meta)
	}
	return out, nil
}

// Close flushes the manifest. It never removes anything: a workspace outlives
// the process that opened it, and removal is an explicit destroy.

func (s *Store) metaFromEntry(bucket, key string, entry ManifestEntry, info ...os.FileInfo) *storage.ObjectMeta {
	meta := storage.ObjectMeta{
		Bucket:            bucket,
		Key:               key,
		ETag:              entry.ETag,
		VersionID:         entry.VersionID,
		ContentType:       entry.ContentType,
		LastModified:      entry.Modified,
		Metadata:          storage.CloneMetadata(entry.Metadata),
		ChecksumAlgorithm: entry.ChecksumAlgorithm,
		ChecksumValue:     entry.ChecksumValue,
	}
	if len(info) > 0 {
		if stat := info[0]; stat != nil {
			meta.Size = stat.Size()
			if meta.LastModified.IsZero() {
				meta.LastModified = stat.ModTime().UTC()
			}
		}
	} else {
		meta.Size = entry.Size
	}
	if meta.VersionID == "" {
		meta.VersionID = meta.ETag
	}
	return &meta
}

// escapedObjects returns the objects the walk in listAll cannot see.
//
// Escaped keys live under the internal directory, which the walk skips because the
// walk skips everything internal. They come from the object index instead, and an entry
// whose file is gone serves as absent rather than as a phantom object. A symlinked one
// is absent for the same reason and not merely unmeasured: it would be listed and then
// sniffed, and the read behind the sniff is one this package refuses everywhere else.
//
// It is a separate function because it is a separate question — what the index says —
// and folding it into listAll pushed that function past its complexity ceiling for a
// check the walk above already makes.
func (s *Store) escapedObjects(bucket string, walked map[string]storage.ObjectMeta) map[string]storage.ObjectMeta {
	found := map[string]storage.ObjectMeta{}
	for key, entry := range s.objectIndex.Buckets[bucket] {
		if entry.Form != FormEscaped {
			continue
		}
		if _, alreadyWalked := walked[key]; alreadyWalked {
			continue
		}
		info, err := os.Lstat(EscapedPath(s.root, bucket, key))
		if err != nil || isLink(info) {
			continue
		}
		file, err := s.openRead(EscapedPath(s.root, bucket, key), info)
		if err != nil {
			continue
		}
		file.Close()
		found[key] = *s.metaFromEntry(bucket, key, entry)
	}
	return found
}

// isLink reports whether a stat describes a symbolic link.
//
// The one question every read path in this package asks before it opens anything. A
// link is a pointer, not content, and its target is not part of the workspace, so
// listAll must not report one and the reads behind a listing must not follow it.
func isLink(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }

// detectContentType sniffs a file's type from its first bytes, because a file
// the host wrote has no declared content type and guessing from the extension
// alone gets text/markdown and application/json wrong often enough to matter.
func detectContentType(path string) string {
	// The same no-follow open GetObject uses. This reads workspace bytes too — the
	// first 512 of them — and a sniff that crosses a link is a read that crosses a
	// link, whatever the caller does with the resulting string.
	file, err := openNoFollow(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(head[:n])
}

// pruneEmptyParents removes directories a delete leaves behind, stopping at the
// bucket directory and never crossing into the internal one.
func (s *Store) pruneEmptyParents(bucket, absPath string) {
	root := s.bucketDir(bucket)
	for dir := filepath.Dir(absPath); strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
		if dir == root {
			return
		}
		if err := s.removeConfined(dir); err != nil {
			return
		}
	}
}
