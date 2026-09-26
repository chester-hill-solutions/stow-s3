package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type Store struct {
	root     string
	layout   Layout
	manifest *Manifest
	// index is the object index, split out of the manifest because it changes per
	// object and the manifest does not. See manifestVersion.
	// objectIndex is the object index, split out of the manifest because it changes
	// per object and the manifest does not. See manifestVersion.
	objectIndex *Index
	workspace   string

	// folded maps a case-folded workspace-relative path, qualified by bucket,
	// to the key that owns it. It is the collision defence from
	// docs/workspace-contract.md section 3.4: on a case-insensitive filesystem
	// two keys differing only in case are one file, and serving both from it
	// loses one silently.
	folded map[string]string

	// Now is the store's clock, replaceable in tests.
	Now func() time.Time

	mu     sync.RWMutex
	closed bool
}

var _ storage.Store = (*Store)(nil)

// Options configures a workspace store.
type Options struct {
	// Root is the workspace directory. It is created if absent, and adopted
	// with whatever it already holds if not.
	Root string
	// Bucket is the workspace's own bucket, whose objects are the root
	// directory itself. Any other bucket is namespaced under the reserved
	// internal directory so that a caller who genuinely wants a second bucket
	// gets bucket isolation without the workspace bucket ceasing to be the
	// working directory.
	Bucket string
	// TTLSeconds records the intended collection window. The collector itself
	// is ADR 0009 work; the value is stored so a future collector and any
	// operator reading the manifest can see what was asked for.
	TTLSeconds int64
	// Now is injectable for tests.
	Now func() time.Time
}

// New opens a workspace store rooted at options.Root.

func New(options Options) (*Store, error) {
	if options.Root == "" {
		return nil, fmt.Errorf("workspace store: Root is required")
	}
	if options.Bucket != "" {
		if err := storage.ValidateBucketName(options.Bucket); err != nil {
			return nil, fmt.Errorf("workspace store: bucket %q: %w", options.Bucket, err)
		}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	// Ownership is decided before anything is created, because it is the only
	// thing that makes Destroy safe. A directory that already existed is
	// adopted, and an adopted workspace is never stow's to delete.
	adopted := directoryExists(options.Root)
	if err := os.MkdirAll(options.Root, 0o755); err != nil {
		return nil, fmt.Errorf("workspace store: create root: %w", err)
	}
	if err := os.MkdirAll(InternalPath(options.Root, "keys"), 0o755); err != nil {
		return nil, fmt.Errorf("workspace store: create internal directory: %w", err)
	}
	manifest, index, err := loadManifest(options.Root)
	if err != nil {
		return nil, err
	}
	store := &Store{
		root:        options.Root,
		layout:      NewLayout(),
		manifest:    manifest,
		objectIndex: index,
		workspace:   options.Bucket,
		folded:      map[string]string{},
		Now:         now,
	}
	if err := store.establishIdentity(options, adopted, now); err != nil {
		return nil, err
	}
	if err := store.index(); err != nil {
		return nil, err
	}
	if err := store.manifest.saveIdentity(); err != nil {
		return nil, err
	}
	return store, store.objectIndex.save()
}

// establishIdentity fills in the identity fields a new workspace does not have
// yet, and preserves the ones it does.
//
// Ownership is set once, by whoever made the directory, and then preserved. A
// directory stow creates right now is owned, full stop. A directory that already
// existed is only owned if the manifest already said so — which happens when stow
// created it on an earlier run and this is a reopen. The distinction has to
// survive the reopen, or a workspace stow created would quietly become
// undeletable the second time it was opened, and Destroy would be useless for
// the case it exists for.
func (s *Store) establishIdentity(options Options, adopted bool, now func() time.Time) error {
	if s.manifest.WorkspaceID == "" {
		id, err := newWorkspaceID()
		if err != nil {
			return err
		}
		s.manifest.WorkspaceID = id
	}
	if s.manifest.Bucket == "" {
		s.manifest.Bucket = options.Bucket
	}
	if s.manifest.TTLSeconds == 0 {
		s.manifest.TTLSeconds = options.TTLSeconds
	}
	s.manifest.Owned = !adopted || s.manifest.Owned
	s.manifest.Created = s.manifest.Created.UTC()
	if s.manifest.Created.IsZero() {
		s.manifest.Created = now().UTC()
	}
	return nil
}

// Now is the store's clock, replaceable in tests.

func newWorkspaceID() (string, error) {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("workspace store: mint id: %w", err)
	}
	return "ws_" + hex.EncodeToString(id[:]), nil
}

// Root is the absolute workspace directory.
func (s *Store) Root() string { return s.root }

// WorkspaceBucket is the bucket whose objects are the workspace directory.
func (s *Store) WorkspaceBucket() string { return s.manifest.Bucket }

// ID is the workspace's durable identity.
func (s *Store) ID() string { return s.manifest.WorkspaceID }

// IsOwned reports whether stow created this workspace directory, as opposed to
// adopting one that already existed. The registry records it so a later sweep
// can tell the two apart without opening every workspace.
func (s *Store) IsOwned() bool { return s.manifest.Owned }

// Path returns where a key's bytes live on disk, and whether they are there.
//
// It exists so a host can hand a caller a real path without an S3 round trip:
// "the report is at /work/output/report.pdf" is the sentence a workspace is
// for. The bool is false when the key is absent, which is the same answer
// GetObject gives, reached without reading a byte.
func (s *Store) Path(bucket, key string) (string, bool) {
	absPath, err := s.locate(bucket, key)
	if err != nil {
		return "", false
	}
	return absPath, true
}

// bucketDir returns the directory a bucket's objects live in. The workspace
// bucket is the root itself; anything else is namespaced under the internal
// directory, which is what lets a caller have a second bucket without the
// workspace bucket ceasing to be the working directory.

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	// Close is one of only two places identity changes, so the identity document
	// is written here alongside the index. The other is creation.
	s.manifest.LastUsed = s.now().UTC()
	if err := s.manifest.saveIdentity(); err != nil {
		return err
	}
	return s.objectIndex.save()
}

func (s *Store) checkOpen() error {
	if s.closed {
		return ErrClosed
	}
	return nil
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// metaFromEntry turns a manifest entry plus a file's stat into object metadata.
// A file with no entry is still an object: size and modification time come from
// the filesystem, and the ETag is the content hash.
