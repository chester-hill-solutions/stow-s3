package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// registryVersion is the only on-disk layout this build implements. A file
// written by a newer revision is refused rather than downgraded, for the same
// reason the manifest is: a registry that silently drops entries forgets
// workspaces, and a registry that forgets a workspace leaks it forever.
const registryVersion = 1

// Entry is the durable record of one workspace, named by its session ID.
//
// This is the thing that makes a workspace outlive the process that created it.
// ADR 0009 section 1 requires a resume to be "opening by this ID", not
// reconstructing state from an environment mapping, and that is only possible if
// the directory and the bucket survive independently of any handle.
type Entry struct {
	ID               string    `json:"id"`
	Dir              string    `json:"dir"`
	WorkingDirectory string    `json:"working_directory,omitempty"`
	Bucket           string    `json:"bucket"`
	Created          time.Time `json:"created"`
	LastUsed         time.Time `json:"last_used"`
	TTLSeconds       int64     `json:"ttl_seconds"`
	// PolicyVersion is zero for registry entries written before resume preserved
	// authority and normalized quota limits. Such entries must not be reopened
	// with today's permissive defaults.
	PolicyVersion      int    `json:"policy_version,omitempty"`
	AuthorityMask      uint32 `json:"authority_mask,omitempty"`
	MaxBytes           int64  `json:"max_bytes,omitempty"`
	MaxObjects         int64  `json:"max_objects,omitempty"`
	MaxCheckpointBytes int64  `json:"max_checkpoint_bytes,omitempty"`
	MaxCheckpoints     int64  `json:"max_checkpoints,omitempty"`
	// Owned mirrors the workspace manifest. The collector uses it to refuse
	// adopted workspaces outright, which is the single most important safety
	// property in this file: an adopted workspace is somebody's project, and no
	// unattended sweep may remove it.
	Owned bool `json:"owned"`
}

// Registry is the set of workspaces on this machine, on disk.
type Registry struct {
	dir string
}

// Dir is the absolute registry location on this machine.
func (r *Registry) Dir() string { return r.dir }

// DefaultRegistryDir is where the registry lives: the user's configuration
// directory, under the package's own name. It holds small metadata only, never
// object data, and it is not inside any workspace — a registry stored in a
// workspace would be collectable by its own sweep.
func DefaultRegistryDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("workspace: locate config directory: %w", err)
	}
	return filepath.Join(base, "stow-s3", "workspaces"), nil
}

// TeamRegistryDir is the registry root for one team: a directory beneath the
// machine's registry that holds that team's entries and that team's checkpoints,
// and nothing of anybody else's.
//
// It is a partition of the directory rather than a field on the entry, and that
// choice is the whole point. A label recorded on an entry is a label: `All` would
// still return every team's workspaces, a sweep would still consider them, and
// two teams sharing a runner would still be one namespace with extra steps. A
// partition is enforced by the filesystem, so a team cannot be resumed, collected,
// or checkpointed from outside its own root, and no code path has to remember to
// check.
//
// The root registry is unaffected. Its entries sit beside the `teams` directory,
// which `All` already skips because it ignores subdirectories, so a machine that
// never names a team behaves exactly as it did.
func TeamRegistryDir(base, team string) (string, error) {
	if base == "" {
		var err error
		if base, err = DefaultRegistryDir(); err != nil {
			return "", err
		}
	}
	if !ValidTeamName(team) {
		return "", fmt.Errorf("workspace: %q is not a usable team name", team)
	}
	return filepath.Join(base, "teams", team), nil
}

// ValidTeamName reports whether a team can be a directory name.
//
// The rules are the portable-path-segment rules the rest of this package already
// applies to checkpoint paths, for the same reason: a team becomes a directory
// that entries are filed under, so a name that can climb out of its namespace or
// that is not portable across the machines that share it is refused rather than
// sanitised. A sanitised name is a name two callers did not agree on.
func ValidTeamName(team string) bool {
	if team == "" || len(team) > 64 || team == "." || team == ".." {
		return false
	}
	for _, r := range team {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return !strings.HasPrefix(team, ".")
}

// OpenRegistry opens the registry at dir, creating it if absent.
func OpenRegistry(dir string) (*Registry, error) {
	if dir == "" {
		return nil, fmt.Errorf("workspace: registry directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: create registry: %w", err)
	}
	return &Registry{dir: dir}, nil
}

// OpenRegistryReadOnly opens a registry without creating its directory.
//
// OpenRegistry makes the directory, which is right for a verb about to write an
// entry and wrong for a verb about to read them: listing would otherwise bring a
// registry into existence on a machine that has never had one, and a caller could
// not tell afterwards whether it had always been there. A missing directory is an
// empty registry, and All already reports that.
func OpenRegistryReadOnly(dir string) (*Registry, error) {
	if dir == "" {
		return nil, fmt.Errorf("workspace: registry directory is required")
	}
	return &Registry{dir: dir}, nil
}

func (r *Registry) entryPath(id string) string {
	return filepath.Join(r.dir, id+".json")
}

// Register records a workspace and returns its entry.
func (r *Registry) Register(entry Entry) error {
	if entry.ID == "" {
		return fmt.Errorf("workspace: registry entry needs an id")
	}
	if entry.Created.IsZero() {
		entry.Created = time.Now().UTC()
	}
	if entry.LastUsed.IsZero() {
		entry.LastUsed = time.Now().UTC()
	}
	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: encode registry entry: %w", err)
	}
	if err := writeFileAtomic(r.entryPath(entry.ID), append(raw, '\n')); err != nil {
		return fmt.Errorf("workspace: record registry entry: %w", err)
	}
	return nil
}

// Lookup returns the entry for a session ID.
func (r *Registry) Lookup(id string) (Entry, bool, error) {
	raw, err := os.ReadFile(r.entryPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("workspace: read registry entry: %w", err)
	}
	entry, err := decodeEntry(raw)
	if err != nil {
		return Entry{}, false, err
	}
	return entry, true, nil
}

// Forget removes a session's record. It is what makes collection and an explicit
// Destroy converge on the same end state: the bytes go, and so does the name
// they were filed under.
func (r *Registry) Forget(id string) error {
	if err := os.Remove(r.entryPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("workspace: forget registry entry: %w", err)
	}
	return nil
}

// All returns every recorded workspace, ordered by ID so a sweep is
// deterministic.
func (r *Registry) All() ([]Entry, error) {
	names, err := os.ReadDir(r.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: list registry: %w", err)
	}
	var entries []Entry
	for _, name := range names {
		if name.IsDir() || filepath.Ext(name.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(r.dir, name.Name()))
		if err != nil {
			continue
		}
		entry, err := decodeEntry(raw)
		if err != nil {
			// A single unreadable entry must not stop the sweep, or one bad file
			// would make every workspace permanent.
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

func decodeEntry(raw []byte) (Entry, error) {
	entry := Entry{}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Entry{}, fmt.Errorf("workspace: decode registry entry: %w", err)
	}
	if entry.ID == "" {
		return Entry{}, fmt.Errorf("workspace: registry entry has no id")
	}
	return entry, nil
}

// Reclaim is what a sweep did, and why.
type Reclaim struct {
	Entry Entry
	// Reason is one of "expired", "adopted", or "in-use", and is what a caller
	// reports to a user. "in-use" and "adopted" are the two that mean the
	// collector declined to act.
	Reason string
}

// Collect removes workspaces that are past their TTL and provably unused.
//
// The order of the checks is the design. Liveness is established before
// anything is considered for deletion, so a workspace that is merely *old* is
// never removed while somebody is in it. Ownership is checked next, so no sweep
// can ever remove a directory a caller pointed stow at. Only then is age
// consulted, and only for workspaces this machine created.
//
// It returns what it declined as well as what it removed, because "nothing was
// collected" and "three workspaces were skipped because they are in use" are
// different answers and a caller diagnosing a leak needs to tell them apart.
func (r *Registry) Collect(now time.Time) ([]Reclaim, error) {
	if !LockSupported() {
		return nil, fmt.Errorf("workspace: cannot collect on this host: %w", ErrLockUnsupported)
	}
	entries, err := r.All()
	if err != nil {
		return nil, err
	}

	var out []Reclaim
	for _, entry := range entries {
		reason, act := r.classify(entry, now)
		out = append(out, Reclaim{Entry: entry, Reason: reason})
		if !act {
			continue
		}
		if err := reclaimWorkspace(entry); err != nil {
			// A workspace that cannot be opened right now — a permission
			// problem, a path that has gone — is recorded and skipped, not
			// fatal. One bad entry must not make the rest permanent.
			out[len(out)-1].Reason = "unreadable: " + err.Error()
			continue
		}
		if err := r.ForgetCheckpoints(entry.ID); err != nil {
			out[len(out)-1].Reason = "unreadable: " + err.Error()
			continue
		}
		if err := r.Forget(entry.ID); err != nil {
			return out, err
		}
	}
	return out, nil
}

// Prune forgets entries whose workspace directory no longer exists.
//
// Not Collect with a different threshold: Collect asks whether a workspace is
// finished with, a judgement about time and ownership that destroys directories.
// Prune asks whether anything is there at all, which is a fact, and removes stow's
// own records and nothing else — so it cannot delete user data, and it needs no lock
// support because there is nothing to quiesce.
//
// Collect cannot reach these by default: a prepared workspace has no TTL and one
// opened on a caller's directory is adopted, so both classify forever and nothing takes
// them out. includeAdopted covers the second. It is off by default because the entry
// is the only record the caller has that they adopted the project, and it is offered
// because the directory is already gone — only the record is at stake, and a registry
// can otherwise grow until a listing is mostly records for directories that are not.
func (r *Registry) Prune(includeAdopted bool) ([]Reclaim, error) {
	entries, err := r.All()
	if err != nil {
		return nil, err
	}
	var out []Reclaim
	for _, entry := range entries {
		reason, act := classifyPrunable(entry, includeAdopted)
		out = append(out, Reclaim{Entry: entry, Reason: reason})
		if !act {
			continue
		}
		if err := r.ForgetCheckpoints(entry.ID); err != nil {
			out[len(out)-1].Reason = "unreadable: " + err.Error()
			continue
		}
		if err := r.Forget(entry.ID); err != nil {
			return out, err
		}
	}
	return out, nil
}

// classifyPrunable decides whether one entry is stow's own debris. A missing
// directory is the only prunable thing, because it is the only one that is a fact
// rather than a judgement; anything present is left for Collect, which owns the
// ownership and age rules this does not want to restate.
func classifyPrunable(entry Entry, includeAdopted bool) (reason string, act bool) {
	_, err := os.Stat(entry.Dir)
	switch {
	case err == nil:
		return "present", false
	case !errors.Is(err, os.ErrNotExist):
		// A permission problem or a path that is not a directory is not evidence the
		// workspace is gone, and treating it as gone would forget a live one.
		return "unreadable: " + err.Error(), false
	case !entry.Owned && !includeAdopted:
		return "gone-adopted", false
	case !entry.Owned:
		return "pruned-adopted", true
	}
	return "pruned", true
}

// classify decides whether one workspace may be removed, and says why.
func (r *Registry) classify(entry Entry, now time.Time) (reason string, act bool) {
	live, err := ProbeLiveness(entry.Dir)
	if err != nil {
		return "unreadable: " + err.Error(), false
	}
	if live {
		return "in-use", false
	}
	// Ownership before age. An adopted workspace is a developer's project, and a
	// sweep that could remove one would be a data-loss bug reachable by waiting.
	if !entry.Owned {
		return "adopted", false
	}
	if entry.TTLSeconds <= 0 {
		return "no-ttl", false
	}
	if now.Sub(entry.LastUsed) < time.Duration(entry.TTLSeconds)*time.Second {
		return "not-expired", false
	}
	return "expired", true
}

// reclaimWorkspace opens a workspace and destroys it, which routes the removal
// through the same ownership and protected-path checks a caller gets. The
// collector has no privilege of its own.
func reclaimWorkspace(entry Entry) error {
	store, err := New(Options{Root: entry.Dir, Bucket: entry.Bucket})
	if err != nil {
		return err
	}
	defer store.Close()
	return store.Destroy()
}
