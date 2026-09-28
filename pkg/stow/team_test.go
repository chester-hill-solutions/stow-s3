package stow_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// twoTeams opens one workspace in each of two team partitions under a shared
// registry root, which is the shape of a runner that serves two jobs. Both are
// returned closed: a sweep has to be able to act on them, and a live session is
// exactly what a sweep must decline to touch.
func twoTeams(t *testing.T) (string, *stow.Workspace, *stow.Workspace) {
	t.Helper()
	base := t.TempDir()
	opened := make([]*stow.Workspace, 0, 2)
	for _, team := range []string{"platform", "product"} {
		ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
			Dir: filepath.Join(base, team+"-task"), RegistryDir: base, Team: team, TTL: time.Hour,
		})
		if err != nil {
			t.Fatalf("open %s workspace: %v", team, err)
		}
		if err := ws.Close(); err != nil {
			t.Fatalf("close %s workspace: %v", team, err)
		}
		opened = append(opened, ws)
	}
	return base, opened[0], opened[1]
}

func sweepAt(t *testing.T, registryDir, team string) []stow.CollectResult {
	t.Helper()
	results, err := stow.Collect(stow.CollectOptions{
		RegistryDir: registryDir, Team: team,
		Now: func() time.Time { return time.Now().Add(2 * time.Hour) },
	})
	if err != nil {
		t.Fatalf("collect %s: %v", team, err)
	}
	return results
}

// A team is a partition of the registry, and the partition is the isolation: the
// entries are filed in different directories, and the checkpoints that go with
// them are not reachable from the other side.
func TestATeamsWorkspacesLiveInSeparatePartitions(t *testing.T) {
	base, platform, product := twoTeams(t)
	platformDir, err := stow.ResolveRegistryDir(base, "platform")
	if err != nil {
		t.Fatal(err)
	}
	productDir, err := stow.ResolveRegistryDir(base, "product")
	if err != nil {
		t.Fatal(err)
	}
	if platformDir == productDir {
		t.Fatalf("both teams resolved to %s", platformDir)
	}
	for _, pair := range []struct{ dir, id string }{
		{platformDir, platform.ID()}, {productDir, product.ID()},
	} {
		if _, err := os.Stat(filepath.Join(pair.dir, pair.id+".json")); err != nil {
			t.Errorf("entry for %s is not filed in its own partition: %v", pair.id, err)
		}
	}
	if _, err := stow.LoadCheckpoint(base, checkpointInPlatformTeam(t, base)); err == nil {
		t.Error("a team's checkpoint was readable from the registry root")
	}
}

// checkpointInPlatformTeam takes a checkpoint through the platform partition, which
// is the only way a caller can reach it: by naming the team.
func checkpointInPlatformTeam(t *testing.T, base string) string {
	t.Helper()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
		Dir: filepath.Join(base, "platform-task"), RegistryDir: base, Team: "platform",
	})
	if err != nil {
		t.Fatalf("reopen platform workspace: %v", err)
	}
	defer ws.Close()
	if err := os.WriteFile(filepath.Join(ws.Dir(), "TASK.md"), []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	return checkpoint.ID
}

// A sweep in one partition must not see, and therefore must not be able to
// remove, a workspace another one owns. A label on an entry would fail here: All
// would still return both.
func TestASweepInOneTeamCannotRemoveAnotherTeamsWorkspace(t *testing.T) {
	base, _, product := twoTeams(t)
	for _, result := range sweepAt(t, base, "platform") {
		if result.ID == product.ID() {
			t.Errorf("the platform sweep considered the product workspace %s", result.ID)
		}
	}
	if _, err := os.Stat(product.Dir()); err != nil {
		t.Errorf("the product workspace was removed by another team's sweep: %v", err)
	}
	if results := sweepAt(t, base, ""); len(results) != 0 {
		t.Errorf("the unteamed root sweep saw %d team workspaces: %+v", len(results), results)
	}
}

// The ID alone must not be enough to find a team's workspace: a caller that
// cannot say which team it wants is asking the root registry, and the answer is
// that it is not there. This is also the check that a resume registers the
// workspace back into the partition it was found in.
func TestResumingATeamWorkspaceRequiresTheTeam(t *testing.T) {
	base, _, product := twoTeams(t)
	if _, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: base}, product.ID()); err == nil {
		t.Error("a product workspace was resumed from the registry root")
	}
	resumed, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: base, Team: "product"}, product.ID())
	if err != nil {
		t.Fatalf("resume in its own team: %v", err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
	for _, result := range sweepAt(t, base, "") {
		if result.ID == product.ID() {
			t.Errorf("resuming a team workspace filed it at the registry root: %s", result.ID)
		}
	}
}

// A registry directory is a root and a team is a partition inside it, so the two
// compose: a runner that keeps every workspace under one directory names that
// directory and a team inside it, and neither is silently preferred.
func TestARegistryRootAndATeamCompose(t *testing.T) {
	base := t.TempDir()
	partition, err := stow.ResolveRegistryDir(base, "platform")
	if err != nil {
		t.Fatalf("ResolveRegistryDir: %v", err)
	}
	if partition != filepath.Join(base, "teams", "platform") {
		t.Fatalf("partition = %s, want it inside the given root", partition)
	}
	root, err := stow.ResolveRegistryDir(base, "")
	if err != nil {
		t.Fatalf("ResolveRegistryDir: %v", err)
	}
	if root != base {
		t.Fatalf("no team resolved to %s, want the root %s", root, base)
	}
}

// The name becomes a directory that entries are filed under, so the rules are the
// portable-segment rules: anything that could climb out of the namespace, or that
// would not survive being spelled on another machine, is refused rather than
// rewritten.
func TestTeamNamesAreValidatedRatherThanRewritten(t *testing.T) {
	for _, team := range []string{".", "..", "platform/other", "platform\\other", ".hidden", "with space", "with:colon", string(make([]byte, 65))} {
		if err := openInTeam(t, team); err == nil {
			t.Errorf("team name %q was accepted", team)
		}
	}
	for _, team := range []string{"platform", "team-1", "team_1", "a.b", "T"} {
		if err := openInTeam(t, team); err != nil {
			t.Errorf("team name %q was refused: %v", team, err)
		}
	}
}

func openInTeam(t *testing.T, team string) error {
	t.Helper()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
		Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: t.TempDir(), Team: team,
	})
	if err != nil {
		return err
	}
	return ws.Close()
}
