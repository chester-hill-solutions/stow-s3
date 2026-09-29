package stow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// Workspace is a bounded artifact workspace: a real directory a caller works
// in, and a bucket over the same bytes. It is the default surface described in
// ADR 0007, and its contract is docs/workspace-contract.md.
//
// The embedded *Runtime supplies the object operations, so quotas and
// accounting have exactly one choke point and a workspace is not a second
// implementation of PutObject.
//
// Closing a workspace releases the handle. It does not delete anything: a
// workspace outlives the process that opened it (ADR 0009), and removal is an
// explicit Destroy.
type Workspace struct {
	*Runtime

	dir                string
	workingDirectory   string
	bucket             string
	id                 string
	store              *workspace.Store
	session            *workspace.Session
	registry           *workspace.Registry
	registryDir        string
	maxCheckpointBytes int64
	maxCheckpoints     int64
	checkpointMu       sync.Mutex
	now                func() time.Time
	closed             bool
	destroyed          bool
}

// WorkspaceOptions configures a workspace.
//
// Dir is the only required field, and it is required on purpose: a workspace is
// a place, not a container of opaque bytes.
type WorkspaceOptions struct {
	// Dir is the workspace directory. It is created if absent, and adopted with
	// whatever it already holds if not, so pointing a workspace at a directory a
	// caller is already working in is the normal case rather than a special one.
	Dir string
	// Bucket overrides the generated workspace bucket name. The bucket's objects
	// are the directory itself, so a key like output/report.pdf is at
	// Dir/output/report.pdf.
	Bucket string
	// MaxBytes and MaxObjects bound the workspace. Zero takes the runtime
	// defaults, so a workspace is bounded unless a caller deliberately lifts
	// the bound.
	MaxBytes   int64
	MaxObjects int64
	// MaxCheckpointBytes and MaxCheckpoints bound the total payload bytes and
	// number of retained checkpoints for this workspace. Zero means unlimited.
	// A cap rejects a new checkpoint; it never evicts an older one.
	MaxCheckpointBytes int64
	MaxCheckpoints     int64
	// Authority is what the workspace permits, enforced below every interface so
	// the filesystem surface and the S3 surface cannot be granted different
	// things. A nil pointer permits everything, which is what a workspace has
	// always done.
	//
	//	readOnly := stow.ReadOnly()
	//	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Authority: &readOnly})
	Authority *Authority
	// TTL is the collection window the workspace records for itself. A collector
	// reclaims a workspace that is past its TTL and whose session is gone; see
	// Collect. The collector is not automatic, so a caller that wants the window
	// honoured has to run one — recording a TTL does not schedule a sweep, and a
	// workspace left past its TTL is retained until something collects it.
	TTL time.Duration
	// RegistryDir places the machine's workspace registry. Empty takes the
	// default under the user's configuration directory. Tests set it so they
	// never touch a real one.
	RegistryDir string
	// Team files this workspace under one team's partition of the registry, so
	// two teams sharing a machine — two jobs on one CI runner, say — never see
	// each other's workspaces, checkpoints, or sweeps. It is a directory
	// partition, not a label: RegistryDir is the root and Team is a directory
	// inside it, so the two compose.
	Team string
	// Adopted marks a workspace the caller is taking over rather than scratch space
	// stow made. It is what keeps Destroy's refusal honest: a caller's project is not
	// stow's to delete.
	Adopted bool
	// Now is injectable for tests.
	Now   func() time.Time
	owned bool
}

// OpenWorkspace opens a workspace rooted at options.Dir.
//
// It starts no process, opens no listener, and reads no ambient environment, so
// there is nothing to clean up on a crash beyond the caller's own exit and
// nothing for a host to leak credentials for. Where a client cannot embed the
// runtime, the scoped S3 session in the TypeScript and Python packages remains
// the supported alternative; see ADR 0007 section 3 for why Python is the
// exception.
func OpenWorkspace(options WorkspaceOptions) (*Workspace, error) {
	if options.Dir == "" {
		return nil, fmt.Errorf("stow: workspace Dir is required")
	}
	if options.MaxCheckpointBytes < 0 || options.MaxCheckpoints < 0 {
		return nil, fmt.Errorf("stow: checkpoint retention limits must not be negative")
	}
	bucket := options.Bucket
	if bucket == "" {
		bucket = generatedBucketName()
	}

	store, err := workspace.New(workspace.Options{
		Root:           options.Dir,
		Bucket:         bucket,
		TTLSeconds:     int64(options.TTL.Seconds()),
		Now:            options.Now,
		InitiallyOwned: options.owned,
		Adopted:        options.Adopted,
	})
	if err != nil {
		return nil, fmt.Errorf("stow: open workspace: %w", err)
	}

	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend:    runtime.BackendWorkspace,
		MaxBytes:   options.MaxBytes,
		MaxObjects: options.MaxObjects,
		Authority:  options.Authority,
	}, store, nil)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("stow: open workspace: %w", err)
	}

	// A live session holds an advisory lock for as long as this handle exists.
	// It is what lets a collector establish that nobody is using the workspace
	// rather than guess, and the kernel releases it even if this process is
	// killed outright.
	session, err := workspace.AcquireSession(store.Root())
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("stow: claim workspace: %w", err)
	}

	ws := &Workspace{
		Runtime:          &Runtime{inner: instance},
		dir:              store.Root(),
		workingDirectory: store.Root(),
		bucket:           store.WorkspaceBucket(),
		id:               store.ID(),
		store:            store,
		session:          session,
	}
	ws.now = options.Now
	ws.maxCheckpointBytes = options.MaxCheckpointBytes
	ws.maxCheckpoints = options.MaxCheckpoints
	// The workspace bucket is bootstrapped through the store rather than through
	// the runtime, because it is a construction step and not a caller operation.
	// The runtime carries the authority the caller asked for, so routing this
	// through it would evaluate the grant against the act of issuing it - and a
	// read-only authority, which withholds bucket.create, could not open a
	// workspace at all.
	//
	// The store returns early for the workspace bucket, so this materialises
	// nothing; it only keeps the grant from being consulted about a bucket the
	// workspace is made of. Widening ReadOnly instead would hand a read-only
	// workspace the ability to create buckets it has no use for.
	if err := store.CreateBucket(context.Background(), ws.bucket); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("stow: create workspace bucket: %w", err)
	}
	// The registry directory is resolved once, here, and the resolved path is what
	// gets recorded. Resolving it per consumer would let the workspace's own entry
	// and a later resume disagree about which partition this workspace belongs to,
	// and a disagreement about a namespace is not recoverable by retrying.
	registryDir, err := ResolveRegistryDir(options.RegistryDir, options.Team)
	if err != nil {
		_ = ws.Close()
		return nil, err
	}
	if err := ws.register(registryDir, int64(options.TTL.Seconds())); err != nil {
		_ = ws.Close()
		return nil, err
	}
	return ws, nil
}

// Dir is the absolute workspace directory, stable for the workspace's life.
func (w *Workspace) Dir() string { return w.dir }

// WorkingDirectory is the directory an external task runner should use as its
// current working directory. Ordinary workspaces default to their root.
func (w *Workspace) WorkingDirectory() string { return w.workingDirectory }

// Bucket is the bucket over the workspace's bytes. Its objects are the directory
// itself, which is what makes a local file and an s3:// key the same bytes.
func (w *Workspace) Bucket() string { return w.bucket }

// ID is the workspace's durable identity, which outlives this handle.
func (w *Workspace) ID() string { return w.id }

// MaxCheckpointBytes is the cumulative payload-byte cap for retained
// checkpoints; zero means unlimited.
func (w *Workspace) MaxCheckpointBytes() int64 { return w.maxCheckpointBytes }

// MaxCheckpoints is the retained checkpoint count cap; zero means unlimited.
func (w *Workspace) MaxCheckpoints() int64 { return w.maxCheckpoints }

// Path returns where a key's bytes live, and whether they are there. It answers
// without an S3 round trip, so a host can print a real path for a caller.
func (w *Workspace) Path(key string) (string, bool) {
	return w.store.Path(w.bucket, key)
}

// captureTarget is this handle expressed as something a capture can read, so the
// handle's own checkpoints and an external one run the same code.
func (w *Workspace) captureTarget() captureTarget {
	return captureTarget{
		dir: w.dir, registryDir: w.registryDir, workspaceID: w.id,
		maxCheckpointBytes: w.maxCheckpointBytes, maxCheckpoints: w.maxCheckpoints,
		now: w.now,
	}
}

// Close releases the handle. It is the non-destructive half of the split in
// ADR 0009 section 3: the process and the client go away, and the bytes do not.
//
// It also releases the session lock, which is what makes the workspace
// collectable again. Closing twice is not an error.
func (w *Workspace) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	var firstErr error
	if err := w.session.Release(); err != nil {
		firstErr = err
	}
	if err := w.Runtime.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// assertOpen refuses work on a closed handle, so a lifecycle mistake is a named
// error rather than a confusing failure from underneath.
func (w *Workspace) assertOpen() error {
	if w.closed {
		return ErrClosed
	}
	return nil
}

// Destroy removes the workspace directory, and only if stow created it.
//
// This is the explicit half of the lifecycle split in ADR 0009 section 3:
// Close releases the handle, Destroy removes the bytes. A workspace stow
// *adopted* — one pointed at a directory the caller already had, which is the
// documented way to use one — is refused, because its contents are the caller's
// and not stow's to delete. A refusal leaves the workspace intact and usable.
//
// A workspace that is already gone is not an error: destroy is idempotent.
func (w *Workspace) Destroy(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := w.store.Destroy(); err != nil {
		return err
	}
	w.destroyed = true
	// The bytes are gone, so the name they were filed under must go too, or the
	// registry accumulates entries that resolve to nothing.
	if w.registry != nil {
		if err := w.registry.ForgetCheckpoints(w.id); err != nil {
			return err
		}
		return w.registry.Forget(w.id)
	}
	return nil
}

// generatedBucketName returns a bucket name unlikely to collide with another
// workspace's. A caller that needs a stable name across reopens passes Bucket
// explicitly or reads it back from an existing workspace.
func generatedBucketName() string {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		// A bucket name that cannot be randomised is still a valid bucket name;
		// the collision risk is far below anything that matters here.
		return "stow-workspace"
	}
	return "stow-workspace-" + hex.EncodeToString(suffix[:])
}
