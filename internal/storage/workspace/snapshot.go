package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// LogicalSnapshot reads object state without opening a writable store or claiming its session.
type LogicalSnapshot struct {
	WorkspaceID   string
	PrimaryBucket string
	Buckets       []string
	Objects       []SnapshotObject
}

type SnapshotObject struct {
	Bucket            string
	Key               string
	Source            string
	Size              int64
	SHA256            string
	Mode              uint32
	ContentType       string
	Metadata          map[string]string
	ChecksumAlgorithm string
	ChecksumValue     string
}

type SnapshotOptions struct {
	MaxBytes   int64
	MaxObjects int64
	Select     func(bucket, key string) bool
}

func ReadLogicalSnapshot(ctx context.Context, root string, options SnapshotOptions) (LogicalSnapshot, error) {
	pinned, err := rooted.Open(root)
	if err != nil {
		return LogicalSnapshot{}, err
	}
	defer pinned.Close()
	manifest, index, err := snapshotDocuments(pinned)
	if err != nil {
		return LogicalSnapshot{}, err
	}
	reader := &logicalSnapshotReader{Store: &Store{root: root, layout: NewLayout(), manifest: manifest, objectIndex: index}, pinned: pinned, sources: map[string]map[string]string{}}
	buckets, err := snapshotBuckets(pinned, manifest.Bucket)
	if err != nil {
		return LogicalSnapshot{}, err
	}
	result := LogicalSnapshot{WorkspaceID: manifest.WorkspaceID, PrimaryBucket: manifest.Bucket, Buckets: buckets}
	var total int64
	for _, bucket := range buckets {
		keys, err := reader.snapshotKeys(bucket, options.MaxObjects)
		if err != nil {
			return LogicalSnapshot{}, err
		}
		for _, key := range keys {
			if options.Select != nil && !options.Select(bucket, key) {
				continue
			}
			if options.MaxObjects > 0 && int64(len(result.Objects)) >= options.MaxObjects {
				return LogicalSnapshot{}, fmt.Errorf("workspace: snapshot object count exceeds limit")
			}
			remaining := snapshotRemainingBytes(options.MaxBytes, total)
			object, err := reader.snapshotObject(ctx, bucket, key, remaining)
			if err != nil {
				return LogicalSnapshot{}, err
			}
			if object.Size > int64(^uint64(0)>>1)-total {
				return LogicalSnapshot{}, fmt.Errorf("workspace: snapshot size overflow")
			}
			total += object.Size
			if options.MaxBytes > 0 && total > options.MaxBytes {
				return LogicalSnapshot{}, fmt.Errorf("workspace: snapshot bytes exceed limit")
			}
			result.Objects = append(result.Objects, object)
		}
	}
	return result, pinned.Check()
}

type logicalSnapshotReader struct {
	*Store
	pinned  *rooted.Root
	sources map[string]map[string]string
}

func snapshotDocuments(root *rooted.Root) (*Manifest, *Index, error) {
	manifest, index := &Manifest{}, &Index{}
	for name, document := range map[string]versioned{"manifest.json": manifest, "index.json": index} {
		input, err := root.OpenRegularFile(filepath.Join(".stow", name))
		if os.IsNotExist(err) && name == "index.json" {
			index.Version = manifestVersion
			index.Buckets = map[string]map[string]ManifestEntry{}
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(input, (16<<20)+1))
		input.Close()
		if err != nil || len(raw) > 16<<20 {
			return nil, nil, fmt.Errorf("workspace: cannot read bounded snapshot %s", name)
		}
		if err := json.Unmarshal(raw, document); err != nil {
			return nil, nil, err
		}
		if document.layoutVersion() != manifestVersion {
			return nil, nil, ErrManifestCorrupt
		}
	}
	if !storage.ValidBucketName(manifest.Bucket) || !ValidWorkspaceID(manifest.WorkspaceID) {
		return nil, nil, ErrManifestCorrupt
	}
	return manifest, index, nil
}

func (s *logicalSnapshotReader) snapshotKeys(bucket string, limit int64) ([]string, error) {
	keys := map[string]bool{}
	s.sources[bucket] = map[string]string{}
	root, err := filepath.Rel(s.root, s.bucketDir(bucket))
	if err != nil {
		return nil, err
	}
	err = fs.WalkDir(s.pinned.FS(), filepath.ToSlash(root), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if IsInternal(filepath.ToSlash(relative)) || strings.EqualFold(entry.Name(), ".git") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace: snapshot refuses symbolic link %q", relative)
		}
		if !entry.IsDir() {
			keys[filepath.ToSlash(relative)] = true
			s.sources[bucket][filepath.ToSlash(relative)] = filepath.Join(s.root, filepath.FromSlash(path))
			if limit > 0 && int64(len(keys)) > limit {
				return fmt.Errorf("workspace: snapshot object count exceeds limit")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.snapshotIndexedKeys(bucket, keys, limit); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

// Natural paths come from the exact directory-entry spelling observed by the walk.
// This preserves case-sensitive object identities without re-reading the whole
// directory for each key. Recorded escaped representations still take precedence.
func (s *logicalSnapshotReader) snapshotIndexedKeys(bucket string, keys map[string]bool, limit int64) error {
	for key, entry := range s.objectIndex.Buckets[bucket] {
		if entry.Form != FormEscaped && s.sources[bucket][key] != "" {
			continue
		}
		source := EscapedPath(s.root, bucket, key)
		relative, err := filepath.Rel(s.root, source)
		if err != nil {
			return err
		}
		file, err := s.pinned.OpenRegularFile(relative)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		file.Close()
		s.sources[bucket][key] = source
		keys[key] = true
		if limit > 0 && int64(len(keys)) > limit {
			return fmt.Errorf("workspace: snapshot object count exceeds limit")
		}
	}
	return nil
}

func (s *logicalSnapshotReader) snapshotObject(ctx context.Context, bucket, key string, maxBytes int64) (SnapshotObject, error) {
	source := s.sources[bucket][key]
	if source == "" {
		return SnapshotObject{}, storage.ErrObjectNotFound
	}
	relative, err := filepath.Rel(s.root, source)
	if err != nil {
		return SnapshotObject{}, err
	}
	input, err := s.pinned.OpenRegularFile(relative)
	if err != nil {
		return SnapshotObject{}, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return SnapshotObject{}, fmt.Errorf("workspace: snapshot requires a regular object")
	}
	if maxBytes >= 0 && info.Size() > maxBytes {
		return SnapshotObject{}, fmt.Errorf("workspace: snapshot bytes exceed limit")
	}
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(snapshotReader{ctx: ctx, reader: input}, info.Size()+1))
	if err != nil {
		return SnapshotObject{}, err
	}
	if size != info.Size() {
		return SnapshotObject{}, fmt.Errorf("workspace: object changed during snapshot")
	}

	entry, _ := s.objectIndex.entry(bucket, key)
	metadata, err := storage.NormalizeUserMetadata(entry.Metadata)
	if err != nil {
		return SnapshotObject{}, err
	}
	object := SnapshotObject{Bucket: bucket, Key: key, Source: source, Size: size, SHA256: hex.EncodeToString(digest.Sum(nil)), Mode: uint32(info.Mode().Perm()), ContentType: entry.ContentType, Metadata: metadata}
	if object.ContentType == "" {
		object.ContentType, err = snapshotContentType(input)
		if err != nil {
			return SnapshotObject{}, err
		}
	}
	object.ChecksumAlgorithm, object.ChecksumValue, err = snapshotChecksum(ctx, input, entry)
	if err != nil {
		return SnapshotObject{}, err
	}
	return object, nil
}

func snapshotChecksum(ctx context.Context, input *os.File, entry ManifestEntry) (string, string, error) {
	if entry.ChecksumAlgorithm == "" || entry.ChecksumValue == "" {
		return "", "", nil
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return "", "", err
	}
	value, err := storage.ComputeChecksumReader(entry.ChecksumAlgorithm, snapshotReader{ctx: ctx, reader: input})
	if err != nil {
		return "", "", err
	}
	if value != entry.ChecksumValue {
		return "", "", nil
	}
	return storage.NormalizeChecksumAlgorithm(entry.ChecksumAlgorithm), value, nil
}

func snapshotContentType(input *os.File) (string, error) {
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	data := make([]byte, 512)
	n, err := input.Read(data)
	if err != nil && err != io.EOF {
		return "", err
	}
	return detectContentTypeFromBytes(data[:n]), nil
}

type snapshotReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r snapshotReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func snapshotBuckets(root *rooted.Root, primary string) ([]string, error) {
	buckets := map[string]bool{primary: true}
	directory, err := root.OpenFile(filepath.Join(".stow", "buckets"))
	if os.IsNotExist(err) {
		return keysOf(buckets), nil
	}
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("workspace: snapshot refuses linked bucket")
		}
		if entry.IsDir() && storage.ValidBucketName(entry.Name()) {
			buckets[entry.Name()] = true
		}
	}
	return keysOf(buckets), nil
}

func snapshotRemainingBytes(limit, used int64) int64 {
	if limit > 0 {
		return limit - used
	}
	return -1
}
