package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// ErrClosed is returned by every operation on a closed store. A workspace store
// is closed when its handle is released, not when its data is removed.
var ErrClosed = errors.New("workspace store is closed")

// locate returns the path a key currently occupies, preferring the form the
// manifest recorded and falling back to whichever file is actually there.
//
// The fallback is what makes adoption work: a file the host wrote has no
// manifest entry, so the only way to find it is to look.
func (s *Store) locate(bucket, key string) (string, error) {
	recorded, hasEntry := s.objectIndex.entry(bucket, key)
	if hasEntry && recorded.Form == FormEscaped {
		path := EscapedPath(s.root, bucket, key)
		if fileExists(path) {
			return path, nil
		}
	}

	if s.layout.IsNatural(key) {
		if path, exists, _ := exactNaturalPath(s.bucketDir(bucket), key); exists {
			return path, nil
		}
	}

	escaped := EscapedPath(s.root, bucket, key)
	if fileExists(escaped) {
		return escaped, nil
	}
	return "", storage.ErrObjectNotFound
}

// exactNaturalPath resolves a natural key without letting a case-insensitive
// filesystem turn a different spelling into the requested object. os.Stat on
// macOS and Windows can succeed for `report.pdf` when only `Report.pdf` exists;
// that is a path collision, not an exact match for the S3 key.
//
// The third result reports that a differently-cased segment occupies the
// candidate path. Callers use it to choose the escaped form for a new object.
func exactNaturalPath(root, key string) (path string, exists, caseCollision bool) {
	path = root
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", false, false
		}
		exactName := ""
		foldedMatch := false
		for _, entry := range entries {
			if entry.Name() == segment {
				exactName = entry.Name()
				break
			}
			if strings.EqualFold(entry.Name(), segment) {
				foldedMatch = true
			}
		}
		if exactName == "" {
			return "", false, foldedMatch
		}
		path = filepath.Join(path, exactName)
		if i == len(segments)-1 {
			return path, fileExists(path), false
		}
	}
	return "", false, false
}

// resolve finds a key's file and its metadata, adopting the file when stow has
// no manifest entry for it. It takes the write lock, because adopting means
// writing a manifest entry.
//
// Adoption is not an import step. There is no separate registration pass,
// because a workspace whose files need registering before they can be read is
// not a working directory.
func (s *Store) resolve(bucket, key string) (string, os.FileInfo, ManifestEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolveLocked(bucket, key)
}

// resolveLocked is resolve for a caller that already holds the write lock.
//
// It exists because the lock is not reentrant and the write path has to look at an
// object while holding it: PutObject peeks to evaluate its preconditions before it
// knows what it is overwriting, and that peek may need to re-derive and persist an
// entry that has gone stale. Reaching resolve from there would take the lock a
// second time on the same goroutine and hang, along with every other goroutine
// waiting behind it - including Close.
//
// This is the same split as putLocked, and it was missing here. The hazard was
// already known when putLocked was separated out for multipart completion, which
// holds the lock for the same reason; the ordinary write was simply not converted
// when it grew the same peek.
func (s *Store) resolveLocked(bucket, key string) (string, os.FileInfo, ManifestEntry, error) {
	absPath, err := s.locate(bucket, key)
	if err != nil {
		return "", nil, ManifestEntry{}, err
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return "", nil, ManifestEntry{}, storage.ErrObjectNotFound
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", nil, ManifestEntry{}, storage.ErrObjectNotFound
	}

	checked, err := s.openRead(absPath, info)
	if err != nil {
		return "", nil, ManifestEntry{}, err
	}
	checked.Close()
	entry, recorded := s.objectIndex.entry(bucket, key)
	if !recorded || entry.stale(info.Size(), info.ModTime()) {
		entry, err = s.derive(absPath, info)
		if err != nil {
			return "", nil, ManifestEntry{}, err
		}
		entry.Form = s.formOf(bucket, key, absPath)
		if entry.Form == FormEscaped {
			entry.Digest = Digest(key)
		}
		// Reuse the recorded version so that re-reading an unchanged file keeps
		// reporting the same version rather than minting one per call.
		if prior, had := s.objectIndex.entry(bucket, key); had {
			entry.VersionID = prior.VersionID
		}
		if err := s.recordLocked(bucket, key, entry); err != nil {
			return "", nil, ManifestEntry{}, err
		}
	}
	return absPath, info, entry, nil
}

// formOf reports which form a path is in, by asking the layout where the key
// would go rather than by inspecting the path.
func (s *Store) formOf(bucket, key, absPath string) Form {
	natural := NaturalPath(s.bucketDir(bucket), key)
	if absPath == natural {
		return FormNatural
	}
	return FormEscaped
}

// derive builds a manifest entry for a file from the file itself: its size and
// modification time from the filesystem, its type from its first bytes, and its
// ETag from its content.
//
// A host-written file has no declared content type and no recorded ETag, and
// inventing either would be worse than deriving them. This is a full read of
// the file, once; the result is cached in the manifest and the read is not
// repeated while size and modification time still match.
func (s *Store) derive(absPath string, info os.FileInfo) (ManifestEntry, error) {
	file, err := s.openRead(absPath, info)
	if err != nil {
		return ManifestEntry{}, err
	}
	return deriveReadFile(file, info)
}

// absorb records a derived entry so the next read does not pay for it again.
func (s *Store) absorb(bucket, key, absPath string, info os.FileInfo, entry *ManifestEntry) error {
	recorded, hasEntry := s.objectIndex.entry(bucket, key)
	if hasEntry && !recorded.stale(info.Size(), info.ModTime()) {
		return nil
	}
	derived, err := s.derive(absPath, info)
	if err != nil {
		return err
	}
	if hasEntry {
		derived.VersionID = recorded.VersionID
		derived.ChecksumAlgorithm = recorded.ChecksumAlgorithm
		derived.ChecksumValue = recorded.ChecksumValue
	}
	derived.Form = s.formOf(bucket, key, absPath)
	if derived.Form == FormEscaped {
		derived.Digest = Digest(key)
	}
	*entry = derived
	return s.record(bucket, key, derived)
}

// record writes an entry to the manifest and the case-folded index, and persists.
// It takes the write lock, so it is only for callers that do not already hold it.
func (s *Store) record(bucket, key string, entry ManifestEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordLocked(bucket, key, entry)
}

// recordLocked is record for a caller that already holds the write lock.
func (s *Store) recordLocked(bucket, key string, entry ManifestEntry) error {
	if s.closed {
		return ErrClosed
	}
	s.objectIndex.setEntry(bucket, key, entry)
	if entry.Form == FormNatural {
		s.folded[foldKey(bucket, key)] = key
	}
	return s.objectIndex.save()
}

// forget drops a key from the manifest and the index. It does not remove bytes;
// the caller does that.
func (s *Store) forget(bucket, key string) {
	s.objectIndex.removeEntry(bucket, key)
	delete(s.folded, foldKey(bucket, key))
}

// peek returns an object's current metadata for a conditional write, or
// ErrObjectNotFound with a nil meta when there is nothing there.
func (s *Store) peek(_ context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	_, info, entry, err := s.resolve(bucket, key)
	if err != nil {
		return nil, err
	}
	meta := s.metaFromEntry(bucket, key, entry, info)
	return meta, nil
}

// peekLocked is peek for a caller that already holds the write lock, for the same
// reason resolveLocked exists.
func (s *Store) peekLocked(bucket, key string) (*storage.ObjectMeta, error) {
	_, info, entry, err := s.resolveLocked(bucket, key)
	if err != nil {
		return nil, err
	}
	return s.metaFromEntry(bucket, key, entry, info), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
