package stow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// The registry is resolved once, and the resolved path is what the reopened
	// workspace is handed. Passing the unresolved pair through instead would let
	// the reopened workspace register itself somewhere other than where it was
	// found — a team workspace re-registered at the registry root is a workspace
	// two sweeps can now both reach.
	registryDir, err := ResolveRegistryDir(options.RegistryDir, options.Team)
	if err != nil {
		return nil, err
	}
	resolved := options
	resolved.RegistryDir = registryDir
	resolved.Team = ""
	registry, err := openRegistry(registryDir, "")
	if err != nil {
		return nil, err
	}
	entry, err := lookupWorkspaceEntry(registry, id)
	if err != nil {
		return nil, err
	}
	policy, maxBytes, maxObjects, maxCheckpointBytes, maxCheckpoints, err := resumedPolicy(entry, resolved)
	if err != nil {
		return nil, err
	}
	ws, err := OpenWorkspace(WorkspaceOptions{
		Dir:                entry.Dir,
		Bucket:             entry.Bucket,
		MaxBytes:           maxBytes,
		MaxObjects:         maxObjects,
		MaxCheckpointBytes: maxCheckpointBytes,
		MaxCheckpoints:     maxCheckpoints,
		Authority:          &policy,
		TTL:                time.Duration(entry.TTLSeconds) * time.Second,
		RegistryDir:        registryDir,
		Now:                resolved.Now,
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

func resumedPolicy(entry workspace.Entry, options WorkspaceOptions) (Authority, int64, int64, int64, int64, error) {
	if entry.PolicyVersion != 1 {
		return Authority{}, 0, 0, 0, 0, fmt.Errorf("stow: workspace %s has no supported persisted access policy; reopen it explicitly with OpenWorkspace or recreate it before resuming", entry.ID)
	}
	policy := Authority{Mask: entry.AuthorityMask}
	if options.Authority != nil {
		if !policy.IsSupersetOf(*options.Authority) {
			return Authority{}, 0, 0, 0, 0, fmt.Errorf("stow: resume authority would widen the workspace policy")
		}
		policy = *options.Authority
	}
	maxBytes, err := resumeLimit("MaxBytes", entry.MaxBytes, options.MaxBytes)
	if err != nil {
		return Authority{}, 0, 0, 0, 0, err
	}
	maxObjects, err := resumeLimit("MaxObjects", entry.MaxObjects, options.MaxObjects)
	if err != nil {
		return Authority{}, 0, 0, 0, 0, err
	}
	maxCheckpointBytes, err := resumeLimit("MaxCheckpointBytes", entry.MaxCheckpointBytes, options.MaxCheckpointBytes)
	if err != nil {
		return Authority{}, 0, 0, 0, 0, err
	}
	maxCheckpoints, err := resumeLimit("MaxCheckpoints", entry.MaxCheckpoints, options.MaxCheckpoints)
	if err != nil {
		return Authority{}, 0, 0, 0, 0, err
	}
	return policy, maxBytes, maxObjects, maxCheckpointBytes, maxCheckpoints, nil
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
// WorkspaceSummary is one workspace as a caller can see it without resuming it.
//
// It is a summary and not the entry, because the question a list answers is "what
// can I pick up", and resuming a workspace to find out is the expensive way to ask.
// Everything here is readable without opening the directory, so listing never
// touches a workspace's files.
type WorkspaceSummary struct {
	ID      string `json:"id"`
	Dir     string `json:"dir"`
	Bucket  string `json:"bucket"`
	Created string `json:"created"`
	// LastUsed is when the workspace was last resumed. A workspace that has not been
	// touched in a month is the one an operator is looking for.
	LastUsed string `json:"last_used"`
	// AgeSeconds and IdleSeconds are the two numbers a sweep or a cleanup decision
	// actually turns on. Both are computed here rather than left to the caller,
	// because "how old is this" has to be measured from one clock to be comparable.
	AgeSeconds  int64 `json:"age_seconds"`
	IdleSeconds int64 `json:"idle_seconds"`
	TTLSeconds  int64 `json:"ttl_seconds"`
	// ExpiresInSeconds is zero when the workspace has no lifetime, which is the
	// default. Zero is therefore ambiguous between "no TTL" and "expires now", so
	// the boolean beside it is what a caller should branch on.
	ExpiresInSeconds int64 `json:"expires_in_seconds"`
	HasTTL           bool  `json:"has_ttl"`
	Owned            bool  `json:"owned"`
	// Readable says whether the directory is still there. A registry entry whose
	// directory has been deleted is the state a crashed or hand-cleaned run leaves
	// behind, and it is the one a list is most useful for finding: the entry is real,
	// the workspace is not, and nothing else reports it.
	Readable bool `json:"readable"`
}

// ListWorkspacesOptions bounds a workspace listing. Named for what it lists rather
// than reusing the runtime's ListOptions, which is an object listing and has a
// Prefix and a Cursor rather than a registry.
type ListWorkspacesOptions struct {
	RegistryDir string
	Team        string
	// Now is the clock, injectable so the ages above are testable rather than
	// dependent on when the suite happens to run.
	Now time.Time
}

// List returns the workspaces this machine knows about, quietest first.
//
// It exists because the alternative was unusable: fifteen verbs, none of which
// enumerates, and LookupWorkspace needs an id the caller must already hold. A
// feature whose whole promise is "your work will still be here" cannot be
// demonstrated to somebody who has not kept a note of the ids.
//
// Quietest first, because the question an operator or an agent has is which
// workspaces have gone stale. A list ordered by creation or by id would answer a
// question nobody asked, and the ids are hashes so that order would look arbitrary.
func List(options ListWorkspacesOptions) ([]WorkspaceSummary, error) {
	resolved, err := ResolveRegistryDir(options.RegistryDir, options.Team)
	if err != nil {
		return nil, err
	}
	// OpenRegistry would create the directory, and a list that creates the thing it
	// lists is a verb that cannot be run against a directory somebody is watching.
	// A missing registry is an empty one, which is what All already reports.
	registry, err := workspace.OpenRegistryReadOnly(resolved)
	if err != nil {
		return nil, fmt.Errorf("stow: read workspace registry: %w", err)
	}
	entries, err := registry.All()
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	summaries := make([]WorkspaceSummary, 0, len(entries))
	for _, entry := range entries {
		summary := WorkspaceSummary{
			ID: entry.ID, Dir: entry.Dir, Bucket: entry.Bucket,
			Created:    entry.Created.UTC().Format(time.RFC3339),
			LastUsed:   entry.LastUsed.UTC().Format(time.RFC3339),
			TTLSeconds: entry.TTLSeconds, Owned: entry.Owned,
			AgeSeconds:  int64(now.Sub(entry.Created).Seconds()),
			IdleSeconds: int64(now.Sub(entry.LastUsed).Seconds()),
			HasTTL:      entry.TTLSeconds > 0,
		}
		if entry.TTLSeconds > 0 {
			summary.ExpiresInSeconds = int64(entry.LastUsed.Add(time.Duration(entry.TTLSeconds) * time.Second).Sub(now).Seconds())
		}
		// Existence only, never an open: listing should not be able to fail because
		// one workspace's directory is unreadable, and it should not be able to
		// create anything.
		if info, statErr := os.Stat(entry.Dir); statErr == nil && info.IsDir() {
			summary.Readable = true
		}
		summaries = append(summaries, summary)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		return summaries[i].IdleSeconds > summaries[j].IdleSeconds
	})
	return summaries, nil
}

func Collect(options CollectOptions) ([]CollectResult, error) {
	registry, err := openRegistry(options.RegistryDir, options.Team)
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
	// Team restricts the sweep to one team's partition. Empty sweeps the root
	// registry, which is every workspace on this machine that is not filed under
	// a team. A team's workspaces are not visible from the root sweep at all,
	// which is the point of the partition: one team's TTL must not decide
	// another team's bytes.
	Team string
	// Now is the clock. Empty takes the wall clock; tests set it.
	Now func() time.Time
}

// openRegistry opens the workspace registry, creating it if absent.
//
// A team partitions that registry by directory, so it is resolved here rather
// than at each call site: every path into a registry goes through this function,
// and a caller that resolved a team by hand anywhere else would be one refactor
// away from silently reading the machine's root registry instead.
func openRegistry(dir, team string) (*workspace.Registry, error) {
	resolved, err := ResolveRegistryDir(dir, team)
	if err != nil {
		return nil, err
	}
	registry, err := workspace.OpenRegistry(resolved)
	if err != nil {
		return nil, fmt.Errorf("stow: open workspace registry: %w", err)
	}
	return registry, nil
}

// ResolveRegistryDir is the one rule for where a workspace's metadata lives.
//
// A registry directory is the root, and a team is a partition inside it, so the
// two compose rather than compete: a runner that keeps its workspaces under one
// directory gives that directory, and a team inside it. An empty team is no team
// and resolves to the root itself, which is what every caller that never heard
// of this is already doing.
func ResolveRegistryDir(dir, team string) (string, error) {
	base, err := orDefaultRegistryDir(dir)
	if err != nil {
		return "", err
	}
	if base == "" {
		return base, nil
	}
	// Absolute, because the answer is recorded in a handoff reference and read by
	// the next process. A relative registry directory means one thing to the
	// process that wrote it and another to the one that reads it, and the
	// difference is a workspace nobody can find.
	absolute, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("stow: resolve registry directory: %w", err)
	}
	if team == "" {
		return absolute, nil
	}
	return workspace.TeamRegistryDir(absolute, team)
}

func orDefaultRegistryDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	defaultDir, err := workspace.DefaultRegistryDir()
	if err != nil {
		return "", err
	}
	return defaultDir, nil
}

// DefaultWorkspaceRegistryDir reports where this host stores local workspace
// references. The registry contains IDs and paths, never S3 credentials.
func DefaultWorkspaceRegistryDir() (string, error) {
	return workspace.DefaultRegistryDir()
}

// DefaultWorkspaceRegistryDirForTeam reports where a named team's workspace
// references live on this host.
func DefaultWorkspaceRegistryDirForTeam(team string) (string, error) {
	base, err := workspace.DefaultRegistryDir()
	if err != nil {
		return "", err
	}
	return workspace.TeamRegistryDir(base, team)
}

// register records a workspace so a later process can resume it. A failure to
// register is reported rather than swallowed: an unregistered workspace cannot
// be resumed, and the caller is the only one who can decide that is acceptable.
func (w *Workspace) register(registryDir string, ttlSeconds int64) error {
	registry, err := openRegistry(registryDir, "")
	if err != nil {
		return err
	}
	w.registry = registry
	w.registryDir = registry.Dir()
	now := w.nowFunc()()
	return registry.Register(workspace.Entry{
		ID:                 w.id,
		Dir:                w.dir,
		WorkingDirectory:   w.workingDirectory,
		Bucket:             w.bucket,
		Created:            now,
		LastUsed:           now,
		TTLSeconds:         ttlSeconds,
		PolicyVersion:      1,
		AuthorityMask:      w.Runtime.Authority().Mask,
		MaxBytes:           w.Runtime.Capabilities().MaxBytes,
		MaxObjects:         w.Runtime.Capabilities().MaxObjects,
		MaxCheckpointBytes: w.maxCheckpointBytes,
		MaxCheckpoints:     w.maxCheckpoints,
		Owned:              w.store.IsOwned(),
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
