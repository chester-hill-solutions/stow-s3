package stow

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// Listing a registry is a different question from reopening a workspace or sweeping
// one, and it earns its own file for the same reason those do: the three share a
// registry and nothing else. A list never opens a workspace, so none of the
// authority, limit, or lock machinery that reopening needs is in scope here, and
// mixing the two is what let a list grow a way of resolving the registry that
// disagreed with the resolver everybody else uses.

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

// WorkspaceListing is what a listing resolved to: the registry it actually read,
// and what that registry holds.
type WorkspaceListing struct {
	// Registry is the directory the entries came from, with the team partition
	// already composed into it. A caller reporting a listing should name the
	// directory it read rather than the pair it passed in, because the two differ
	// whenever a team was given — and a report naming the pair names something that
	// was never opened. The resolution happens here, once, so it cannot be done
	// twice and disagree.
	Registry string
	// Team is the partition within Registry, empty for the root.
	Team string
	// Workspaces is every entry, quietest first, including the ones whose directory
	// is gone. The broken entries are not filtered out here: Readable is on every
	// summary, so a caller can drop them in one line, and a caller that has them
	// dropped for it does not know they existed. See WorkspaceSummary.Readable.
	Workspaces []WorkspaceSummary
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
func List(options ListWorkspacesOptions) (WorkspaceListing, error) {
	resolved, err := ResolveRegistryDir(options.RegistryDir, options.Team)
	if err != nil {
		return WorkspaceListing{}, err
	}
	// OpenRegistry would create the directory, and a list that creates the thing it
	// lists is a verb that cannot be run against a directory somebody is watching.
	// A missing registry is an empty one, which is what All already reports.
	registry, err := workspace.OpenRegistryReadOnly(resolved)
	if err != nil {
		return WorkspaceListing{}, fmt.Errorf("stow: read workspace registry: %w", err)
	}
	entries, err := registry.All()
	if err != nil {
		return WorkspaceListing{}, err
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
	return WorkspaceListing{
		Registry:   resolved,
		Team:       options.Team,
		Workspaces: summaries,
	}, nil
}
