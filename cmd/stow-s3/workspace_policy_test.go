package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/policystore"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// The CLI is where a deployment actually points a policy, so a flag that loads one is
// the difference between the operation being deniable and being deniable in principle.
// These cases pin that the flag reaches the decision, and that without it nothing is
// consulted: the legacy behaviour is preserved rather than quietly tightened.

// runCheckpointVerb runs the verb and returns its error rather than failing on it, so
// a refusal is an outcome to assert. The existing helper is the other shape: it fatals
// on error because most verbs are expected to succeed.
func runCheckpointVerb(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	commandErr := checkpointWorkspaceCommand(args)
	_ = writer.Close()
	os.Stdout = previous
	output, _ := io.ReadAll(reader)
	_ = reader.Close()
	return output, commandErr
}

// persistedPolicy writes a revision denying capture of one workspace and returns the
// store path. It goes through the real store, because the point is that a revision
// somebody persisted elsewhere is usable here.
func persistedPolicy(t *testing.T, registry, workspaceID string, effect policy.Effect) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	store, err := policystore.Open(path)
	if err != nil {
		t.Fatalf("open policy store: %v", err)
	}
	var set policy.Set
	set.Revision = "rev-cli-capture"
	set.Add(policy.LocalNamespace, workspaceID,
		policy.Selector{Kind: policy.KindWorkspace}, effect, authority.WorkspaceCapture)
	record, err := policy.FromSet(set, 0, time.Now(), authority.All())
	if err != nil {
		t.Fatalf("FromSet: %v", err)
	}
	if _, err := store.Persist(record, 0); err != nil {
		t.Fatalf("persist: %v", err)
	}
	return path
}

func cliWorkspace(t *testing.T) (registry, id, dir string) {
	t.Helper()
	registry = filepath.Join(t.TempDir(), "registry")
	dir = filepath.Join(t.TempDir(), "task")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: dir, RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	id = ws.ID()
	if err := ws.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TASK.md"), []byte("work"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return registry, id, dir
}

func TestTheCheckpointVerbRefusesWhenThePolicyDeniesIt(t *testing.T) {
	registry, id, _ := cliWorkspace(t)
	path := persistedPolicy(t, registry, id, policy.Deny)

	_, err := runCheckpointVerb(t, "--id", id, "--registry-dir", registry, "--policy", path)
	if err == nil {
		t.Fatal("workspace checkpoint succeeded under a policy that denies capturing")
	}
	if !strings.Contains(err.Error(), "policy in force") {
		t.Errorf("workspace checkpoint = %v, want a refusal naming the policy", err)
	}
}

func TestTheSamePolicyAllowsTheCheckpointVerb(t *testing.T) {
	// The pairing that distinguishes a real check from default denial, exactly as on
	// the library path: with capture denied, a check for the wrong operation is still
	// refused, so only the allow case tells them apart.
	registry, id, _ := cliWorkspace(t)
	path := persistedPolicy(t, registry, id, policy.Allow)

	if _, err := runCheckpointVerb(t, "--id", id, "--registry-dir", registry, "--policy", path); err != nil {
		t.Errorf("workspace checkpoint = %v under a policy that allows it", err)
	}
}

func TestWithoutTheFlagTheCheckpointVerbConsultsNothing(t *testing.T) {
	// A policy denying capture exists on disk and is simply not named, so a caller
	// who has not opted in gets the behaviour they had. Tightening this silently would
	// break the CLI contract the docs describe.
	registry, id, _ := cliWorkspace(t)
	persistedPolicy(t, registry, id, policy.Deny)

	if _, err := runCheckpointVerb(t, "--id", id, "--registry-dir", registry); err != nil {
		t.Errorf("workspace checkpoint = %v with no --policy, want the unbound behaviour", err)
	}
}

func TestAnUnreadablePolicyPathIsAnErrorNotARefusal(t *testing.T) {
	// "I do not know" and "you may not" are different answers, and a caller sent the
	// wrong one will treat an outage as a permission decision.
	registry, id, _ := cliWorkspace(t)
	_, err := runCheckpointVerb(t, "--id", id, "--registry-dir", registry,
		"--policy", filepath.Join(t.TempDir(), "absent.json"))
	if err == nil {
		t.Fatal("workspace checkpoint succeeded with an absent policy path")
	}
	if policy.IsRefusal(err) {
		t.Errorf("workspace checkpoint = %v, and an unreadable policy must not read as a refusal", err)
	}
}
