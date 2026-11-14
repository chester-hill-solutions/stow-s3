package stow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// Workspace is a real working directory and bucket over the same bytes (ADR 0007).
// Its Runtime owns object operations and accounting; see docs/workspace-contract.md.
// Close preserves data; explicit registered removal refuses active read attachments.
type Workspace struct {
	lifecycleMu sync.Mutex
	facade      *workspaceServer
	*Runtime

	dir                string
	workingDirectory   string
	bucket             string
	id                 string
	store              *workspace.Store
	instance           *runtime.Instance
	policyConfigured   bool
	editor             *WorkspaceEditor
	session            *workspace.Session
	registry           *workspace.Registry
	registryDir        string
	maxCheckpointBytes int64
	maxCheckpoints     int64
	now                func() time.Time
	lifecycle          workspaceLifecycle
}

// WorkspaceOptions configures a workspace with a required directory.
type WorkspaceOptions struct {
	maxRegistryWorkspaces int64
	// Dir is created if absent and adopted with existing contents otherwise.
	// It remains an ordinary directory available to host tools.
	Dir string
	// Bucket overrides the generated workspace bucket name. The bucket's objects
	// are the directory itself, so a key like output/report.pdf is at
	// Dir/output/report.pdf.
	Bucket string
	// MaxBytes and MaxObjects bound the workspace. Zero takes the runtime
	// defaults, so a workspace is bounded unless a caller deliberately lifts
	// the bound.
	MaxBytes            int64
	MaxObjects          int64
	MaxMultipartUploads int64
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
	// Policy narrows Authority per resource, which for a workspace is the registered
	// workspace itself — its Destroy, and eventually its captures. A nil value means no
	// policy is consulted, which is what a workspace has always done. It is a
	// policy.Source, so a revocation takes effect without a reopen.
	Policy policy.Source
	// TTL records eligibility for explicit Collect after writer and read attachments
	// close. Collection is not automatic; an expired workspace stays until a
	// collector runs.
	TTL time.Duration
	// RegistryDir places the machine's workspace registry. Empty takes the
	// default under the user's configuration directory. Tests set it so they
	// never touch a real one.
	RegistryDir string
	// Team partitions RegistryDir into directories for workspaces and checkpoints.
	// This is storage organization, not a trusted per-caller identity or ACL.
	Team string
	// Adopted directories belong to callers and are never deleted by Destroy.
	Adopted bool
	// Now is injectable for tests.
	Now          func() time.Time
	owned        bool
	ownerSession *workspace.Session
}

// OpenWorkspace opens options.Dir without a process, listener or ambient credentials.
// It claims the writer session; OpenWorkspaceReader attaches without that claim.
// TypeScript/Python callers can use the scoped CLI wrappers; see ADR 0007.
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
	session, rootExisted, err := claimWorkspaceSession(options)
	if err != nil {
		return nil, err
	}
	sessionHeld := true
	defer func() {
		if sessionHeld {
			_ = session.Release()
		}
	}()

	store, err := workspace.New(workspace.Options{
		Root:           options.Dir,
		Bucket:         bucket,
		TTLSeconds:     int64(options.TTL.Seconds()),
		Now:            options.Now,
		InitiallyOwned: options.owned || !rootExisted,
		Adopted:        options.Adopted,
	})
	if err != nil {
		return nil, fmt.Errorf("stow: open workspace: %w", err)
	}

	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend:             runtime.BackendWorkspace,
		MaxBytes:            options.MaxBytes,
		MaxObjects:          options.MaxObjects,
		MaxMultipartUploads: options.MaxMultipartUploads,
		Authority:           options.Authority,
		Policy:              options.Policy,
	}, store, nil)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("stow: open workspace: %w", err)
	}

	ws := &Workspace{
		Runtime:          embedded.WrapInstance(instance),
		dir:              store.Root(),
		workingDirectory: store.Root(),
		bucket:           store.WorkspaceBucket(),
		id:               store.ID(),
		store:            store,
		instance:         instance,
		policyConfigured: options.Policy != nil,
		session:          session,
	}
	ws.now = options.Now
	ws.maxCheckpointBytes = options.MaxCheckpointBytes
	ws.maxCheckpoints = options.MaxCheckpoints
	// Bootstrap through the store: opening a read-only handle must not require
	// bucket.create. The workspace bucket already exists logically and this call
	// materializes nothing.
	if err := store.CreateBucket(context.Background(), ws.bucket); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("stow: create workspace bucket: %w", err)
	}
	// Resolve this partition once so registration, resume and read attachments agree.
	// Team is part of the trusted registry path, not identity authorization.
	registryDir, err := ResolveRegistryDir(options.RegistryDir, options.Team)
	if err != nil {
		_ = ws.Close()
		return nil, err
	}
	if err := ws.register(registryDir, int64(options.TTL.Seconds()), options.maxRegistryWorkspaces); err != nil {
		_ = ws.Close()
		return nil, err
	}
	sessionHeld = false
	return ws, nil
}

func claimWorkspaceSession(options WorkspaceOptions) (*workspace.Session, bool, error) {
	if options.ownerSession != nil {
		return options.ownerSession, true, nil
	}
	_, err := os.Lstat(options.Dir)
	rootExisted := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("stow: inspect workspace root: %w", err)
	}
	session, err := workspace.AcquireSession(options.Dir)
	if err != nil {
		return nil, false, fmt.Errorf("stow: claim workspace: %w", err)
	}
	return session, rootExisted, nil
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
		dir: w.dir, registryDir: w.registryDir, workspaceID: w.id, bucket: w.bucket,
		maxCheckpointBytes: w.maxCheckpointBytes, maxCheckpoints: w.maxCheckpoints,
		now: w.now,
	}
}

// Destroy removes the workspace directory, and only if stow created it. This is the
// explicit half of the lifecycle split in ADR 0009 section 3: Close releases the
// handle, Destroy removes the bytes. A workspace stow *adopted* — one pointed at a
// directory the caller already had, which is the documented way to use one — is
// refused, because its contents are the caller's and not stow's to delete, and a
// refusal leaves it intact and usable. A workspace that is already gone is not an
// error: destroy is idempotent. The handle must permit EnvironmentDestroy, closed or
// not.
func (w *Workspace) Destroy(ctx context.Context) error {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	// The environment answers whether destroying is permitted at all and the policy
	// answers whether this workspace is: a caller may be granted destroy over every
	// workspace and denied it for this one. The resource is the root, so a subtree
	// selector does not match and the destroy is refused rather than narrowed.
	if err := w.Runtime.Authority().Check(authority.EnvironmentDestroy); err != nil {
		return err
	}
	if err := w.Runtime.Authorize(authority.EnvironmentDestroy, policy.Workspace(w.id, "")); err != nil {
		return err
	}
	return w.destroyLocked()
}

// discardUncommitted rolls back a constructor's newly created workspace before
// it is returned to a caller. It shares ownership and exclusion checks with
// Destroy, but cleanup does not consume the authority granted to the caller.
func (w *Workspace) discardUncommitted() error {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	return w.destroyLocked()
}

func (w *Workspace) destroyLocked() error {
	remove := func() error {
		if w.store.IsOwned() {
			if err := w.closeRuntimeLocked(); err != nil {
				return err
			}
		}
		return w.store.Destroy()
	}
	if w.registry != nil {
		if err := w.registry.RemoveWorkspace(w.id, remove); err != nil {
			return err
		}
	} else if err := remove(); err != nil {
		return err
	}
	return w.closeLocked()
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
