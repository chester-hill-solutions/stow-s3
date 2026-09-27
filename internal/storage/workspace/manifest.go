package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
)

// manifestVersion is the only on-disk manifest layout this build implements. A
// file written by a newer revision is refused rather than migrated on read: the
// outbox already established that rule for durable files, and a manifest that
// silently downgrades loses exactly the entries it could not understand.
//
// Version 2 splits the document. Version 1 held identity and the object index in
// one file, so adopting one host-written object rewrote the whole workspace's
// metadata: 20 HEADs produced 20 full rewrites, each serialising a document that
// grew as it went. Identity changes once per workspace and the index changes per
// object, so they are separate files with separate write paths.
//
// A version 1 workspace is refused rather than migrated. Its recorded checksums,
// ETags and metadata would have to be carried into index.json by a rewrite that
// can be interrupted with the index as the only copy, and the alternative -
// treating a missing index as empty - discards them silently. Neither is worth it
// for a store whose contents are disposable and whose owner is usually mid-task.
//
// Version 3 namespaces the escaped form by bucket. An escaped key used to be stored
// at a digest of the key alone, so the same key in two buckets resolved to one file
// and the buckets read, wrote and deleted over each other. The fix moved those
// files, and that is what forces the version rather than the other way round.
//
// A version 2 workspace is refused for the same reason version 1 is, and it is worth
// being explicit about why refusing beats opening it. Its manifest is still valid -
// the path was always derived from the key, never recorded - but its escaped objects
// now sit at a path this build does not look at. Opening it would make every
// escaped key resolve to absent while its file stayed on disk, which is the
// total-loss reading that ErrManifestCorrupt's own comment calls the one thing
// worse than losing bytes. A clear refusal is the only safe answer. Version 2 was
// never released, so what this costs is a developer's local workspace.
const manifestVersion = 3

// ErrManifestCorrupt is returned when a manifest exists and cannot be trusted.
//
// It is a refusal, not a repair. The filesystem, not the manifest, is the source
// of truth for what exists, so an empty manifest would not lose bytes — but it
// would make every key resolve to absent while every file stayed on disk, which
// reads to the caller as total data loss. Refusing is the only safe answer.
var ErrManifestCorrupt = errors.New("workspace manifest is corrupt")

// Manifest is the identity document at a workspace root: which workspace this is,
// which bucket it serves, when it was created, how long it lives, and whether stow
// owns the directory.
//
// It used to carry the object index too. That is index.json now, because the two
// change at completely different rates and writing them together made every object
// adoption rewrite the workspace's identity.
//
// It is not the source of truth for existence: an entry pointing at a missing file
// serves as absent, and a file with no entry exists. Losing either document
// degrades metadata, never data.
type Manifest struct {
	Version     int       `json:"version"`
	WorkspaceID string    `json:"workspace_id"`
	Bucket      string    `json:"bucket"`
	Created     time.Time `json:"created"`
	LastUsed    time.Time `json:"last_used"`
	TTLSeconds  int64     `json:"ttl_seconds"`
	// Owned records whether stow created the workspace directory or adopted one
	// that already existed. It is the only thing that makes Destroy safe: a
	// workspace is *meant* to be pointed at a caller's existing directory, so the
	// presence of a manifest is not evidence that the contents are stow's to
	// delete. An adopted workspace can be read, written and closed; removing it
	// is the caller's business.
	Owned bool `json:"owned"`

	path string
}

// Index is the object index: what stow knows about each key it has seen. It is
// rewritten whenever an object is adopted, updated or forgotten, and it is the
// only document on the hot write path.
type Index struct {
	Version int                                 `json:"version"`
	Buckets map[string]map[string]ManifestEntry `json:"buckets"`

	path string
}

// ManifestEntry records what stow knows about one object. A missing entry is
// legal and is the normal case for a file the host wrote.
type ManifestEntry struct {
	Form              Form              `json:"form"`
	Digest            string            `json:"digest,omitempty"`
	Size              int64             `json:"size"`
	ETag              string            `json:"etag,omitempty"`
	VersionID         string            `json:"version_id,omitempty"`
	ContentType       string            `json:"content_type,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	ChecksumAlgorithm string            `json:"checksum_algorithm,omitempty"`
	ChecksumValue     string            `json:"checksum_value,omitempty"`
	Modified          time.Time         `json:"modified"`
}

// manifestPath is where a workspace keeps its identity document.
func manifestPath(root string) string {
	return InternalPath(root, "manifest.json")
}

// indexPath is where a workspace keeps its object index.
func indexPath(root string) string {
	return InternalPath(root, "index.json")
}

// loadManifest reads a workspace's identity document and its object index. A
// workspace with neither yet is not corrupt; it is new, and empty ones are
// returned so the caller can populate them. A document that exists and cannot be
// parsed, or that carries a version this build does not implement, is refused.
//
// A version 1 manifest is refused here, and the message says what to do about it.
// Refusing a workspace whose files are all still on disk would otherwise read as
// data loss, and the fix is one directory removal.
func loadManifest(root string) (*Manifest, *Index, error) {
	// Version is seeded so a workspace with no document yet is already at the
	// current layout; an existing file overwrites it on unmarshal.
	manifest := &Manifest{Version: manifestVersion}
	if err := loadDocument(manifestPath(root), manifest); err != nil {
		return nil, nil, err
	}
	manifest.path = manifestPath(root)

	index := &Index{Version: manifestVersion}
	if err := loadDocument(indexPath(root), index); err != nil {
		return nil, nil, err
	}
	if index.Buckets == nil {
		index.Buckets = map[string]map[string]ManifestEntry{}
	}
	index.path = indexPath(root)
	return manifest, index, nil
}

// versioned is the one thing loadDocument needs from a document: the layout
// version it was written with.
type versioned interface{ layoutVersion() int }

func (m *Manifest) layoutVersion() int { return m.Version }

func (i *Index) layoutVersion() int { return i.Version }

// loadDocument reads one versioned JSON document. Its absence is not corruption —
// a new workspace has neither — but a version this build does not implement is
// refused rather than migrated.
func loadDocument(path string, into versioned) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrManifestCorrupt, filepath.Base(path), err)
	}
	if into.layoutVersion() != manifestVersion {
		return fmt.Errorf("%w: %s is version %d, this build implements %d; this workspace must be "+
			"removed and recreated — its files are still on disk and nothing else needs doing",
			ErrManifestCorrupt, filepath.Base(path), into.layoutVersion(), manifestVersion)
	}
	return nil
}

// save writes the object index, and only the object index.
//
// It is on the hot path - every adoption, update and forget lands here - and it is
// deliberately not the identity document. Identity changes once per workspace;
// before the split, adopting one host-written object rewrote it.
func (i *Index) save() error {
	if i.Buckets == nil {
		i.Buckets = map[string]map[string]ManifestEntry{}
	}
	raw, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace index: %w", err)
	}
	return writeFileAtomic(i.path, append(raw, '\n'))
}

// saveIdentity writes the identity document. It is on the cold path: creation,
// and Close, which is the only other thing that changes LastUsed.
func (m *Manifest) saveIdentity() error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace manifest: %w", err)
	}
	return writeFileAtomic(m.path, append(raw, '\n'))
}

// writeFileAtomic delegates to internal/atomicfile.
//
// It used to be a second implementation of the same thing, and a weaker one: it
// renamed without syncing the parent directory, so every workspace object and
// manifest write could be lost to a power cut that the filesystem backend
// survived. It also chmod'ed the temporary file after the sync and after the
// close, leaving a window in which the file was on disk with the wrong mode.
//
// All five callers in this package go through here, so the guarantee is now
// shared rather than per-file.
func writeFileAtomic(path string, data []byte) error {
	return atomicfile.Write(path, data, 0o644)
}

// entry returns what the index knows about one key, if anything.
func (i *Index) entry(bucket, key string) (ManifestEntry, bool) {
	keys, ok := i.Buckets[bucket]
	if !ok {
		return ManifestEntry{}, false
	}
	entry, ok := keys[key]
	return entry, ok
}

// setEntry records what stow knows about one key.
func (i *Index) setEntry(bucket, key string, entry ManifestEntry) {
	keys, ok := i.Buckets[bucket]
	if !ok {
		keys = map[string]ManifestEntry{}
		i.Buckets[bucket] = keys
	}
	keys[key] = entry
}

// removeEntry forgets a key. The bytes are removed separately; this only stops
// stow claiming to know about something that is gone.
func (i *Index) removeEntry(bucket, key string) {
	keys, ok := i.Buckets[bucket]
	if !ok {
		return
	}
	delete(keys, key)
}

// stale reports whether a recorded entry no longer describes the file on disk,
// which is what an agent overwriting a file behind stow's back looks like.
//
// Size and modification time are compared rather than the ETag because checking
// the ETag would mean reading every file on every head.
func (e ManifestEntry) stale(size int64, modified time.Time) bool {
	if e.Size != size {
		return true
	}
	return !e.Modified.Equal(modified.UTC().Truncate(time.Second))
}
