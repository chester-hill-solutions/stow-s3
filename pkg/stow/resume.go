package stow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// Resume reopens a workspace by the identity it was given when it was created.
//
// This is the whole of ADR 0009 section 1: a workspace outlives the process that
// made it, and reopening is "open by this ID" rather than reconstructing state
// from an environment mapping. An agent that was preempted comes back, asks the
// registry where its work lives, and carries on with the same bytes.
func Resume(id string) (*Workspace, error) {
	return resumeWith(WorkspaceOptions{}, id)
}

// ResumeIn is Resume against a chosen registry. It exists so a caller with its
// own registry location — a host that keeps workspaces somewhere deliberate —
// does not have to adopt the user's configuration directory.
func ResumeIn(registryDir, id string) (*Workspace, error) {
	return ResumeWith(WorkspaceOptions{RegistryDir: registryDir}, id)
}

// ResumeWith is Resume with options, chiefly so the registry location and the
// clock can be chosen. The clock matters more than it looks: a TTL is the one
// behaviour here that cannot be tested by sleeping, so it is tested by
// simulating time instead.
func ResumeWith(options WorkspaceOptions, id string) (*Workspace, error) {
	return resumeWith(options, id)
}

// resumeWith is Resume with options.
func resumeWith(options WorkspaceOptions, id string) (*Workspace, error) {
	registry, err := openRegistry(options.RegistryDir)
	if err != nil {
		return nil, err
	}
	entry, err := lookupWorkspaceEntry(registry, id)
	if err != nil {
		return nil, err
	}
	policy, maxBytes, maxObjects, err := resumedPolicy(entry, options)
	if err != nil {
		return nil, err
	}
	ws, err := OpenWorkspace(WorkspaceOptions{
		Dir:         entry.Dir,
		Bucket:      entry.Bucket,
		MaxBytes:    maxBytes,
		MaxObjects:  maxObjects,
		Authority:   &policy,
		TTL:         time.Duration(entry.TTLSeconds) * time.Second,
		RegistryDir: options.RegistryDir,
		Now:         options.Now,
	})
	if err != nil {
		return nil, err
	}
	if ws.ID() != id {
		// The directory is authoritative, so this means the registry and the
		// workspace disagree about who they are. Carrying on would silently
		// hand back a different workspace than the one that was asked for.
		_ = ws.Close()
		return nil, fmt.Errorf("stow: workspace %s does not match the workspace registered at %s", id, entry.Dir)
	}
	if err := restoreWorkingDirectory(ws, registry, entry); err != nil {
		_ = ws.Close()
		return nil, err
	}
	return ws, nil
}

func lookupWorkspaceEntry(registry *workspace.Registry, id string) (workspace.Entry, error) {
	entry, found, err := registry.Lookup(id)
	if err != nil {
		return workspace.Entry{}, fmt.Errorf("stow: look up workspace %s: %w", id, err)
	}
	if !found {
		return workspace.Entry{}, fmt.Errorf("stow: no workspace with id %s", id)
	}
	return entry, nil
}

func resumedPolicy(entry workspace.Entry, options WorkspaceOptions) (Authority, int64, int64, error) {
	if entry.PolicyVersion != 1 {
		return Authority{}, 0, 0, fmt.Errorf("stow: workspace %s has no supported persisted access policy; reopen it explicitly with OpenWorkspace or recreate it before resuming", entry.ID)
	}
	policy := Authority{Mask: entry.AuthorityMask}
	if options.Authority != nil {
		if !policy.IsSupersetOf(*options.Authority) {
			return Authority{}, 0, 0, fmt.Errorf("stow: resume authority would widen the workspace policy")
		}
		policy = *options.Authority
	}
	maxBytes, err := resumeLimit("MaxBytes", entry.MaxBytes, options.MaxBytes)
	if err != nil {
		return Authority{}, 0, 0, err
	}
	maxObjects, err := resumeLimit("MaxObjects", entry.MaxObjects, options.MaxObjects)
	if err != nil {
		return Authority{}, 0, 0, err
	}
	return policy, maxBytes, maxObjects, nil
}

func resumeLimit(name string, persisted, requested int64) (int64, error) {
	if requested < 0 {
		return 0, fmt.Errorf("stow: resume %s must not be negative", name)
	}
	if requested == 0 {
		return persisted, nil
	}
	if requested > persisted {
		return 0, fmt.Errorf("stow: resume %s would widen the workspace limit", name)
	}
	return requested, nil
}

func restoreWorkingDirectory(ws *Workspace, registry *workspace.Registry, original workspace.Entry) error {
	workingDirectory := original.WorkingDirectory
	if workingDirectory == "" {
		workingDirectory = original.Dir
	}
	rootAbs, err := filepath.Abs(original.Dir)
	if err != nil {
		return fmt.Errorf("stow: resolve workspace root: %w", err)
	}
	workAbs, err := filepath.Abs(workingDirectory)
	if err != nil || !pathWithin(rootAbs, workAbs) {
		return fmt.Errorf("stow: registered working directory is invalid or outside the workspace")
	}
	info, err := os.Stat(workAbs)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("stow: registered working directory is not an existing directory")
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("stow: resolve workspace root: %w", err)
	}
	workReal, err := filepath.EvalSymlinks(workAbs)
	if err != nil || !pathWithin(rootReal, workReal) {
		return fmt.Errorf("stow: registered working directory resolves outside the workspace")
	}
	ws.workingDirectory = workAbs
	current, err := lookupWorkspaceEntry(registry, ws.id)
	if err != nil {
		return fmt.Errorf("stow: restore working directory in workspace registry: %w", err)
	}
	current.WorkingDirectory = workAbs
	if err := registry.Register(current); err != nil {
		return fmt.Errorf("stow: restore working directory in workspace registry: %w", err)
	}
	return nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CollectResult is one workspace a sweep considered, and what it did about it.
type CollectResult struct {
	ID string
	// Dir is the workspace directory, whether or not it survived.
	Dir string
	// Removed is true only when the bytes are gone.
	Removed bool
	// Reason explains a decision. "expired" means it was collected; "in-use",
	// "adopted", "not-expired" and "no-ttl" all mean the sweep declined, and
	// "unreadable: …" means it could not be opened and was left alone.
	Reason string
}

// Collect reclaims workspaces that are past their TTL and provably unused.
//
// The two refusals are the point. A workspace a live session holds is never
// removed, because it is a working directory and deleting it destroys the
// artifact in progress. A workspace stow *adopted* is never removed, because it
// is a caller's own project and no unattended sweep may delete one — that check
// comes before the age check for exactly that reason.
//
// It reports every decision, not only the removals, because "nothing was
// collected" and "three were skipped because they are in use" are different
// answers and a caller diagnosing a leaked workspace has to tell them apart.
//
// Collect is a no-op with an explanatory error on a host that cannot establish
// liveness, rather than guessing.
func Collect(options CollectOptions) ([]CollectResult, error) {
	registry, err := openRegistry(options.RegistryDir)
	if err != nil {
		return nil, err
	}
	if !workspace.LockSupported() {
		return nil, fmt.Errorf("stow: cannot collect on this host: %w", workspace.ErrLockUnsupported)
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	results, err := registry.Collect(now())
	if err != nil {
		return nil, err
	}
	out := make([]CollectResult, 0, len(results))
	for _, result := range results {
		out = append(out, CollectResult{
			ID:      result.Entry.ID,
			Dir:     result.Entry.Dir,
			Removed: result.Reason == "expired",
			Reason:  result.Reason,
		})
	}
	return out, nil
}

// CollectOptions configures a sweep.
type CollectOptions struct {
	// RegistryDir places the machine's workspace registry. Empty takes the
	// default under the user's configuration directory.
	RegistryDir string
	// Now is the clock. Empty takes the wall clock; tests set it.
	Now func() time.Time
}

// openRegistry opens the workspace registry, creating it if absent.
func openRegistry(dir string) (*workspace.Registry, error) {
	if dir == "" {
		defaultDir, err := workspace.DefaultRegistryDir()
		if err != nil {
			return nil, err
		}
		dir = defaultDir
	}
	registry, err := workspace.OpenRegistry(dir)
	if err != nil {
		return nil, fmt.Errorf("stow: open workspace registry: %w", err)
	}
	return registry, nil
}

// DefaultWorkspaceRegistryDir reports where this host stores local workspace
// references. The registry contains IDs and paths, never S3 credentials.
func DefaultWorkspaceRegistryDir() (string, error) {
	return workspace.DefaultRegistryDir()
}

// register records a workspace so a later process can resume it. A failure to
// register is reported rather than swallowed: an unregistered workspace cannot
// be resumed, and the caller is the only one who can decide that is acceptable.
func (w *Workspace) register(registryDir string, ttlSeconds int64) error {
	registry, err := openRegistry(registryDir)
	if err != nil {
		return err
	}
	w.registry = registry
	w.registryDir = registry.Dir()
	now := w.nowFunc()()
	return registry.Register(workspace.Entry{
		ID:               w.id,
		Dir:              w.dir,
		WorkingDirectory: w.workingDirectory,
		Bucket:           w.bucket,
		Created:          now,
		LastUsed:         now,
		TTLSeconds:       ttlSeconds,
		PolicyVersion:    1,
		AuthorityMask:    w.Runtime.Authority().Mask,
		MaxBytes:         w.Runtime.Capabilities().MaxBytes,
		MaxObjects:       w.Runtime.Capabilities().MaxObjects,
		Owned:            w.store.IsOwned(),
	})
}

// Touch records that the workspace was used, which is what a TTL is measured
// from. A workspace an agent keeps returning to is not scratch.
func (w *Workspace) Touch() error {
	if w.registry == nil {
		return nil
	}
	if err := w.assertOpen(); err != nil {
		return err
	}
	entry, found, err := w.registry.Lookup(w.id)
	if err != nil || !found {
		return err
	}
	entry.LastUsed = w.nowFunc()()
	return w.registry.Register(entry)
}

func (w *Workspace) nowFunc() func() time.Time {
	if w.now != nil {
		return w.now
	}
	return time.Now
}
