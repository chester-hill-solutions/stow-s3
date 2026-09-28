package stow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestZeroMaxWorkspacesIsUnlimited(t *testing.T) {
	registry := t.TempDir()
	for i := 0; i < 5; i++ {
		prepareInto(t, registry, filepath.Join(registry, "ws"+string(rune('a'+i))))
	}
	prepareInto(t, registry, filepath.Join(registry, "ws6"))
}

func TestPrepareRefusesWhenTheRegistryIsFull(t *testing.T) {
	registry := t.TempDir()
	prepareInto(t, registry, filepath.Join(registry, "ws1"))
	prepareInto(t, registry, filepath.Join(registry, "ws2"))

	_, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(registry, "ws3"), RegistryDir: registry},
		Inputs:           []stow.WorkspaceInput{seedInput(t, "ws3")},
		MaxWorkspaces:    2,
	})
	if err == nil {
		t.Fatal("a third workspace was prepared into a registry bounded at two")
	}
	// Both commands, because a refusal naming neither leaves the caller guessing
	// whether the registry is full or broken.
	for _, want := range []string{"2 of 2", "workspace prune", "workspace collect", "max_workspaces"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(registry, "ws3")); !os.IsNotExist(statErr) {
		t.Error("the refused prepare left a directory behind, so the refusal cost a cleanup as well as a command")
	}
}

// A test showing only that a second team succeeds proves nothing about scoping: it
// also passes when the bound is applied to the root, which is under the limit for an
// unrelated reason. The refusal is what pins it.
func TestTheBoundIsOnTheTeamPartitionNotTheRoot(t *testing.T) {
	registry := t.TempDir()
	// Fill platform to a bound of two.
	prepareBounded(t, registry, "platform", 2)
	prepareBounded(t, registry, "platform", 2)

	// A third for platform is refused, so the bound reaches that partition at all.
	if err := prepareRefused(t, registry, "platform", 2); err == nil {
		t.Fatal("platform was given a third workspace by a bound of two")
	}
	// billing is empty and succeeds. The root holds two entries and the bound is
	// two, so a bound applied to the root would have refused here — and on a shared
	// runner that is one team consuming another's quota.
	prepareBounded(t, registry, "billing", 2)
}

func TestTheBoundCountsRecordsWhoseDirectoriesAreGone(t *testing.T) {
	registry := t.TempDir()
	prepareInto(t, registry, filepath.Join(registry, "live"))
	if err := os.RemoveAll(filepath.Join(registry, "live")); err != nil {
		t.Fatalf("remove the workspace directory: %v", err)
	}

	_, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(registry, "next"), RegistryDir: registry},
		Inputs:           []stow.WorkspaceInput{seedInput(t, "next")},
		MaxWorkspaces:    1,
	})
	if err == nil {
		t.Fatal("a workspace was prepared into a registry whose only record points at a deleted directory")
	}
	if !strings.Contains(err.Error(), "1 of 1") {
		t.Errorf("the refusal %q does not report the count the bound was judged on", err)
	}
}

// prepareBounded fails the test if the prepare is refused, so a helper used to fill
// a partition to its limit cannot quietly stop filling it.
func prepareBounded(t *testing.T, registry, team string, max int64) *stow.PreparedWorkspace {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ws")
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: dir, RegistryDir: registry, Team: team},
		Inputs:           []stow.WorkspaceInput{seedInput(t, "seed")},
		MaxWorkspaces:    max,
	})
	if err != nil {
		t.Fatalf("prepare into team %q under a bound of %d: %v", team, max, err)
	}
	t.Cleanup(func() { prepared.Workspace.Close() })
	return prepared
}

func prepareRefused(t *testing.T, registry, team string, max int64) error {
	t.Helper()
	_, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "ws"), RegistryDir: registry, Team: team},
		Inputs:           []stow.WorkspaceInput{seedInput(t, "seed")},
		MaxWorkspaces:    max,
	})
	if err == nil {
		t.Fatalf("prepare into team %q under a bound of %d succeeded, and this test exists because it must not", team, max)
	}
	return err
}

func seedInput(t *testing.T, name string) stow.WorkspaceInput {
	t.Helper()
	seed := filepath.Join(t.TempDir(), "seed.txt")
	if err := os.WriteFile(seed, []byte(name), 0o600); err != nil {
		t.Fatalf("write the seed: %v", err)
	}
	return stow.WorkspaceInput{Source: seed, Destination: "seed.txt"}
}

func prepareInto(t *testing.T, registry, dir string, extra ...string) *stow.PreparedWorkspace {
	t.Helper()
	options := stow.WorkspaceOptions{Dir: dir, RegistryDir: registry}
	if len(extra) == 2 {
		options.Team = extra[0]
	}
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: options,
		Inputs:           []stow.WorkspaceInput{seedInput(t, filepath.Base(dir))},
	})
	if err != nil {
		t.Fatalf("prepare %s: %v", dir, err)
	}
	t.Cleanup(func() { prepared.Workspace.Close() })
	return prepared
}
