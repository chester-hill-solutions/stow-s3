package stow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// Three exported functions on a module that resolves on proxy.golang.org, with no
// callers and no tests: DefaultWorkspaceRegistryDir, DefaultWorkspaceRegistryDirForTeam
// and AllowNone. A reviewer finds that in seconds and it reads as surface nobody
// wanted. They are pinned here rather than removed because removing exported symbols
// from a module people can already resolve is the more expensive mistake of the two,
// and because two of them are load-bearing for the leak this session closed — they are
// exactly the functions that decide where a registry lives, and a test is the thing
// that stops that decision moving silently.
//
// Each is asserted on its contract, not on its current value: what it resolves to is
// the caller's business, and a test that asserted a path would fail the next person to
// change the layout.

func TestDefaultWorkspaceRegistryDirIsUnderTheConfigDirectory(t *testing.T) {
	resolved, err := stow.DefaultWorkspaceRegistryDir()
	if err != nil {
		t.Fatalf("resolve the default registry: %v", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("locate the config directory: %v", err)
	}
	if !isUnder(resolved, configDir) {
		t.Errorf("the default registry is %s, which is not under the config directory %s: a caller resolving it has to be able to tell where the answer came from", resolved, configDir)
	}
	if !strings.HasSuffix(resolved, filepath.Join("stow-s3", "workspaces")) {
		t.Errorf("the default registry is %s, and the product's own directory name is part of what callers persist and read back", resolved)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("the default registry is %s, and a relative path means one thing to the process that recorded it and another to the one that reads it", resolved)
	}
}

// ForTeam is the function that makes a team's partition a directory boundary, which
// is the property the delta refusal across workspaces depends on. The assertion is
// that the team is a path segment and that two teams never collide — not what the
// segment is called, which is an implementation detail of the partition layout.
func TestDefaultWorkspaceRegistryDirForTeamPartitions(t *testing.T) {
	base, err := stow.DefaultWorkspaceRegistryDir()
	if err != nil {
		t.Fatalf("resolve the default registry: %v", err)
	}
	platform, err := stow.DefaultWorkspaceRegistryDirForTeam("platform")
	if err != nil {
		t.Fatalf("resolve the platform partition: %v", err)
	}
	research, err := stow.DefaultWorkspaceRegistryDirForTeam("research")
	if err != nil {
		t.Fatalf("resolve the research partition: %v", err)
	}
	if platform == research {
		t.Error("two teams resolve to the same directory, so a team's partition is a label rather than a boundary and the delta refusal has nothing to enforce")
	}
	if platform == base {
		t.Error("a team's partition is the root, so a team cannot be isolated from the root registry")
	}
	for _, pair := range []struct{ team, resolved string }{{"platform", platform}, {"research", research}} {
		if !isUnder(pair.resolved, base) {
			t.Errorf("team %q resolved to %s, which is not under the root %s", pair.team, pair.resolved, base)
		}
		if !strings.Contains(pair.resolved, pair.team) {
			t.Errorf("team %q resolved to %s, which does not mention the team, so the path does not read as the partition it is", pair.team, pair.resolved)
		}
	}
}

// AllowNone is the deny-everything authority. Its whole value is that it denies, so a
// test that only checked it constructs is testing nothing: the property is that every
// operation is refused, and that refusal does not depend on which operation is asked
// about.
func TestAllowNonePermitsNothing(t *testing.T) {
	none := stow.AllowNone()
	if none.Allows(stow.ObjectRead) {
		t.Error("AllowNone permits object read, which is the operation it exists to refuse")
	}
	for _, operation := range []stow.Operation{
		stow.ObjectRead, stow.ObjectWrite, stow.ObjectDelete, stow.ObjectList,
		stow.BucketCreate, stow.BucketDelete, stow.BucketList,
		stow.EnvironmentReset, stow.EnvironmentDestroy, stow.EnvironmentPromote,
		stow.UpstreamRead, stow.UpstreamWrite,
	} {
		if none.Allows(operation) {
			t.Errorf("AllowNone permits %s", operation)
		}
	}
}
