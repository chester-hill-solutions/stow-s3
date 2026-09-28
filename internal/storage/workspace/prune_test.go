package workspace_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// Prune removes stow's own records for directories that are gone. The risk is not
// that it removes too little — the classification is three cases and they are
// readable — it is that it removes something it should have kept, or that it treats
// "I cannot tell" as "it is gone".

func newPruneRegistry(t *testing.T) (*workspace.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	registry, err := workspace.OpenRegistryReadOnly(dir)
	if err != nil {
		t.Fatalf("open the registry: %v", err)
	}
	return registry, dir
}

func registerForPrune(t *testing.T, registry *workspace.Registry, id, dir string, owned bool) {
	t.Helper()
	err := registry.Register(workspace.Entry{
		ID: id, Dir: dir, Bucket: id, Created: time.Now(), LastUsed: time.Now(), Owned: owned,
	})
	if err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
}

func TestPruneRemovesAnOwnedEntryWhoseDirectoryIsGone(t *testing.T) {
	registry, _ := newPruneRegistry(t)
	registerForPrune(t, registry, "ws_gone", filepath.Join(t.TempDir(), "never-existed"), true)

	results, err := registry.Prune(false)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(results) != 1 || results[0].Reason != "pruned" {
		t.Fatalf("results = %+v, want one pruned", results)
	}
	if _, present, _ := registry.Lookup("ws_gone"); present {
		t.Error("the entry is still in the registry after being pruned")
	}
}

// An entry whose directory is still there is never prunable, whatever its ownership
// or age. Prune has no opinion about either, and Collect owns those rules.
func TestPruneKeepsAnEntryWhoseDirectoryIsPresent(t *testing.T) {
	registry, _ := newPruneRegistry(t)
	live := t.TempDir()
	registerForPrune(t, registry, "ws_live", live, true)
	registerForPrune(t, registry, "ws_live_adopted", live, false)

	results, err := registry.Prune(true)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, result := range results {
		if result.Reason != "present" {
			t.Errorf("%s classified %q, want present: its directory is still there", result.Entry.ID, result.Reason)
		}
	}
	if _, present, _ := registry.Lookup("ws_live"); !present {
		t.Error("a workspace whose directory is present was removed")
	}
}

// The adopted protection is the default because the entry is the only remaining
// record that the caller adopted the project. It is a flag rather than a decision
// because the directory is already gone, so nothing but the record is at stake.
func TestPruneKeepsAGoneAdoptedEntryUnlessAsked(t *testing.T) {
	registry, _ := newPruneRegistry(t)
	gone := filepath.Join(t.TempDir(), "hand-deleted")
	registerForPrune(t, registry, "ws_adopted", gone, false)

	kept, err := registry.Prune(false)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if kept[0].Reason != "gone-adopted" {
		t.Errorf("reason = %q, want gone-adopted", kept[0].Reason)
	}
	if _, present, _ := registry.Lookup("ws_adopted"); !present {
		t.Fatal("an adopted entry was removed without being asked: the record that the project was ever adopted is not stow's to throw away")
	}

	taken, err := registry.Prune(true)
	if err != nil {
		t.Fatalf("Prune with includeAdopted: %v", err)
	}
	if taken[0].Reason != "pruned-adopted" {
		t.Errorf("reason = %q, want pruned-adopted, so a caller can tell it from stow's own debris", taken[0].Reason)
	}
	if _, present, _ := registry.Lookup("ws_adopted"); present {
		t.Error("--include-adopted left the entry in place")
	}
}

// "I cannot tell" is not "it is gone". A path that exists but cannot be stat'd for
// any other reason must be left alone, because forgetting it would lose a workspace
// that is still there.
func TestPruneKeepsAnEntryWhoseDirectoryIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can stat a directory with no permissions, so this cannot be arranged here")
	}
	registry, _ := newPruneRegistry(t)
	base := t.TempDir()
	unreadable := filepath.Join(base, "locked")
	if err := os.Mkdir(unreadable, 0o000); err != nil {
		t.Fatalf("create the unreadable directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })
	registerForPrune(t, registry, "ws_locked", unreadable, true)

	results, err := registry.Prune(true)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want one", results)
	}
	if results[0].Reason == "pruned" || results[0].Reason == "pruned-adopted" {
		t.Errorf("reason = %q: a directory that cannot be read is not evidence that it is gone", results[0].Reason)
	}
	if _, present, _ := registry.Lookup("ws_locked"); !present {
		t.Error("an entry whose directory could not be read was removed")
	}
}

// Prune is a read and a tidy-up, so it must not bring a registry into existence. A
// tool that creates the thing it is tidying cannot be run against a directory
// somebody is watching.
func TestPruneDoesNotCreateARegistryThatIsNotThere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created-yet")
	registry, err := workspace.OpenRegistryReadOnly(missing)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := registry.Prune(false); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("pruning created the registry directory at %s", missing)
	}
}
