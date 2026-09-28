package stow_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// There were fifteen workspace verbs and none of them listed. Every other verb needs
// an id, and the only way to get one was to have kept a note of it — so a feature
// whose whole promise is "your work will still be here" could not be shown to
// anybody who had not.
//
// These tests pin the three things a listing has to get right, and each of them is a
// decision rather than an implementation detail.

// writeSeed creates the one input every prepared workspace needs.
func writeSeed(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".md")
	if err := os.WriteFile(path, []byte("input for "+name), 0o600); err != nil {
		t.Fatalf("write the seed for %s: %v", name, err)
	}
	return path
}

// newListedWorkspace prepares a workspace and returns its id.
func newListedWorkspace(t *testing.T, registryDir, root, name string) string {
	t.Helper()
	ws, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{
			Dir: root, RegistryDir: registryDir, Bucket: name,
		},
		// Prepare refuses a workspace with no inputs, which is a real constraint the
		// listing has no opinion about. A listing test that worked around it by
		// preparing nothing would be testing a workspace that cannot exist.
		Inputs: []stow.WorkspaceInput{{
			Source: writeSeed(t, name), Destination: "spec.md",
		}},
	})
	if err != nil {
		t.Fatalf("prepare %s: %v", name, err)
	}
	if err := ws.Workspace.Close(); err != nil {
		t.Fatalf("close %s: %v", name, err)
	}
	return ws.Workspace.ID()
}

func TestListNamesEveryWorkspaceWithoutResumingOne(t *testing.T) {
	registry := t.TempDir()
	base := t.TempDir()
	first := newListedWorkspace(t, registry, filepath.Join(base, "one"), "one")
	second := newListedWorkspace(t, registry, filepath.Join(base, "two"), "two")

	summaries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("the listing holds %d workspaces, want 2: %+v", len(summaries), summaries)
	}
	ids := map[string]stow.WorkspaceSummary{}
	for _, summary := range summaries {
		ids[summary.ID] = summary
	}
	for _, want := range []string{first, second} {
		summary, present := ids[want]
		if !present {
			t.Fatalf("the listing does not name %s: %+v", want, ids)
		}
		if !summary.Readable {
			t.Errorf("%s is listed as unreadable, but it was just prepared", want)
		}
		if summary.Bucket == "" {
			t.Errorf("%s is listed with no bucket, which is the one field a caller needs to reach it", want)
		}
	}
}

func TestListReportsAWorkspaceWhoseDirectoryIsGone(t *testing.T) {
	// The state a crashed or hand-cleaned run leaves behind: the registry entry is
	// real, the directory is not. Nothing else in the product reports it, and it is
	// the thing somebody listing workspaces is looking for.
	registry := t.TempDir()
	base := t.TempDir()
	kept := newListedWorkspace(t, registry, filepath.Join(base, "kept"), "kept")
	removed := newListedWorkspace(t, registry, filepath.Join(base, "removed"), "removed")
	if err := os.RemoveAll(filepath.Join(base, "removed")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	summaries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	readable := map[string]bool{}
	for _, summary := range summaries {
		readable[summary.ID] = summary.Readable
	}
	if !readable[kept] {
		t.Error("the surviving workspace is listed as unreadable")
	}
	if readable[removed] {
		t.Error("a workspace whose directory is gone is listed as readable, so a list cannot find the state a crashed run leaves")
	}
}

func TestListIsOrderedByHowQuietEachWorkspaceIs(t *testing.T) {
	// The question an operator or an agent has is which workspaces have gone quiet.
	// A list ordered by creation or by id would answer a question nobody asked, and
	// the ids are hashes, so that order would look arbitrary.
	//
	// The assertion is on the ordering property rather than on which workspace
	// lands first, because two workspaces prepared microseconds apart have almost
	// the same idle time and a test that named the expected winner would be
	// asserting the scheduler.
	registry := t.TempDir()
	base := t.TempDir()
	newListedWorkspace(t, registry, filepath.Join(base, "one"), "one")
	time.Sleep(1100 * time.Millisecond)
	newListedWorkspace(t, registry, filepath.Join(base, "two"), "two")

	summaries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("the listing holds %d workspaces, want 2", len(summaries))
	}
	for i := 1; i < len(summaries); i++ {
		if summaries[i].IdleSeconds > summaries[i-1].IdleSeconds {
			t.Fatalf("entry %d is less idle than entry %d, so the list is not quietest-first: %+v", i, i-1, summaries)
		}
	}
	// And the workspace prepared first is the quieter one, which is what makes the
	// ordering observable rather than merely monotonic.
	if summaries[0].Dir != filepath.Join(base, "one") {
		t.Errorf("the quieter workspace is %s, want the one prepared first (%s)",
			summaries[0].Dir, filepath.Join(base, "one"))
	}
}

func TestListOrdersByIdleSecondsUsingAnInjectedClock(t *testing.T) {
	// The ordering is a claim about idle time, so it is checked against an injected
	// clock rather than against wall time. A test that reads the real clock can only
	// assert that the order did not change, which passes on a list ordered by
	// anything at all.
	registry := t.TempDir()
	base := t.TempDir()
	newListedWorkspace(t, registry, filepath.Join(base, "alpha"), "alpha")
	newListedWorkspace(t, registry, filepath.Join(base, "beta"), "beta")

	now := time.Now()
	summaries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry, Now: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, summary := range summaries {
		// An hour after creation, and neither has been resumed, so both are an hour
		// idle. Equal values must still produce a stable order rather than depending
		// on map iteration, which is what the slice sort above guarantees.
		if summary.IdleSeconds < 3500 || summary.IdleSeconds > 3700 {
			t.Errorf("%s reports %d idle seconds against an injected clock an hour on, want about 3600",
				summary.ID, summary.IdleSeconds)
		}
	}
}

func TestListDistinguishesNoLifetimeFromExpiringNow(t *testing.T) {
	// expires_in_seconds is zero both when a workspace has no TTL — the default —
	// and when it is expiring this second. A caller branching on the number alone
	// would sweep half a registry by accident, so the boolean is what it branches on.
	registry := t.TempDir()
	base := t.TempDir()
	newListedWorkspace(t, registry, filepath.Join(base, "forever"), "forever")

	summaries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("the listing holds %d workspaces, want 1", len(summaries))
	}
	if summaries[0].HasTTL {
		t.Error("a workspace with no TTL is reported as having one")
	}
	if summaries[0].ExpiresInSeconds != 0 {
		t.Errorf("a workspace with no TTL reports %d seconds until expiry, want 0",
			summaries[0].ExpiresInSeconds)
	}
}

func TestListKeepsTeamsApart(t *testing.T) {
	// A team's partition is a directory boundary, so listing one team must not show
	// another's workspaces. This is the property that makes a shared runner safe, and
	// it is the same boundary the delta refusal depends on.
	registry := t.TempDir()
	base := t.TempDir()
	platform, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{
			Dir: filepath.Join(base, "p"), RegistryDir: registry, Team: "platform", Bucket: "platform-one",
		},
		Inputs: []stow.WorkspaceInput{{Source: writeSeed(t, "platform-one"), Destination: "spec.md"}},
	})
	if err != nil {
		t.Fatalf("prepare platform: %v", err)
	}
	_ = platform.Workspace.Close()
	other, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{
			Dir: filepath.Join(base, "o"), RegistryDir: registry, Team: "research", Bucket: "research-one",
		},
		Inputs: []stow.WorkspaceInput{{Source: writeSeed(t, "research-one"), Destination: "spec.md"}},
	})
	if err != nil {
		t.Fatalf("prepare research: %v", err)
	}
	_ = other.Workspace.Close()

	platformList, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry, Team: "platform"})
	if err != nil {
		t.Fatalf("list platform: %v", err)
	}
	if len(platformList) != 1 || platformList[0].ID != platform.Workspace.ID() {
		t.Fatalf("listing the platform team returned %+v, want only %s", platformList, platform.Workspace.ID())
	}
	researchList, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry, Team: "research"})
	if err != nil {
		t.Fatalf("list research: %v", err)
	}
	if len(researchList) != 1 || researchList[0].ID != other.Workspace.ID() {
		t.Fatalf("listing the research team returned %+v, want only %s", researchList, other.Workspace.ID())
	}
}

func TestListOfAnEmptyRegistryIsEmptyRatherThanAnError(t *testing.T) {
	// A machine that has never prepared a workspace has nothing to list, and that is
	// a fact rather than a failure — otherwise the verb cannot be used in a script
	// that has not created anything yet.
	summaries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: t.TempDir()})
	if err != nil {
		t.Fatalf("listing an empty registry: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("an empty registry listed %+v", summaries)
	}
}

func TestListNeverCreatesAnything(t *testing.T) {
	// Listing is a read. It must not create the registry, the workspace directories,
	// or any marker inside them — a verb that "tidies up" on the way past is a verb
	// that cannot be run against a directory somebody is watching.
	registry := filepath.Join(t.TempDir(), "not-created-yet")
	if _, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := os.Stat(registry); !os.IsNotExist(err) {
		t.Fatalf("listing created the registry directory at %s", registry)
	}
}
