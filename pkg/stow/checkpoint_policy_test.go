package stow_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// A capture produces a durable, exportable artefact containing the workspace's
// bytes, so it changes state even though it discloses nothing a reader could not
// already read. That is why workspace.capture is an operation rather than a reuse of
// object.read, and why ReadOnly withholds it.
//
// These cases pin the three things that make the operation mean something: a policy
// that denies capture stops the capture before the lock is taken, an allow leaves it
// working, and the environment still answers first.

// captureWorkspace prepares a workspace whose runtime carries a policy, and hands
// back the ID the policy has to name. The ID is generated, so a host cannot write a
// policy against one before the workspace exists; it opens, learns the registered
// identity, and writes the policy after. The tests do the same thing.
func captureWorkspace(t *testing.T, build func(*policy.Set, string)) (*stow.Workspace, string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "task")
	registry := filepath.Join(t.TempDir(), "registry")

	first, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: dir, RegistryDir: registry})
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	id := first.ID()
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var set policy.Set
	build(&set, id)
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
		Dir:         dir,
		RegistryDir: registry,
		Policy:      policy.Fixed(&set),
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws, id, registry
}

func captureWholeWorkspace(s *policy.Set, id string, effect policy.Effect, ops ...authority.Operation) {
	s.Add(policy.LocalNamespace, id, policy.Selector{Kind: policy.KindWorkspace}, effect, ops...)
}

func TestAPolicyCanDenyCapturingAWorkspace(t *testing.T) {
	ws, _, _ := captureWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-deny-capture"
		theWholeWorkspace(s, id, policy.Deny, authority.WorkspaceCapture)
	})
	_, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err == nil {
		t.Fatal("CreateCheckpoint succeeded for a workspace a policy denies capturing")
	}
	// A policy refusal rather than the environment's, because the environment grants
	// everything here and a caller sent to its Authority would be sent to the wrong place.
	if !strings.Contains(err.Error(), "policy in force") {
		t.Errorf("CreateCheckpoint = %v, want a refusal naming the policy", err)
	}
}

func TestTheSamePolicyWithAllowLeavesCaptureWorking(t *testing.T) {
	// Identical to the case above but for the effect, so the previous test cannot
	// pass for any reason the workspace happened to be uncapturable.
	ws, _, _ := captureWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-allow-capture"
		theWholeWorkspace(s, id, policy.Allow, authority.WorkspaceCapture)
	})
	if _, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{}); err != nil {
		t.Errorf("CreateCheckpoint = %v under a policy that allows it", err)
	}
}

func TestCaptureStillNeedsTheEnvironmentGrant(t *testing.T) {
	// A policy that grants capture cannot conjure a grant the environment withheld.
	readOnly := authority.ReadOnly()
	dir := filepath.Join(t.TempDir(), "task")
	registry := filepath.Join(t.TempDir(), "registry")

	probe, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: dir, RegistryDir: registry})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id := probe.ID()
	if err := probe.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var set policy.Set
	set.Revision = "rev-allow-capture"
	theWholeWorkspace(&set, id, policy.Allow, authority.WorkspaceCapture)
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
		Dir:         dir,
		RegistryDir: registry,
		Authority:   &readOnly,
		Policy:      policy.Fixed(&set),
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer ws.Close()

	_, err = ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err == nil {
		t.Fatal("CreateCheckpoint succeeded under a read-only environment because a policy granted it")
	}
	if strings.Contains(err.Error(), "policy in force") {
		t.Errorf("CreateCheckpoint = %v, want the environment named as the thing that refused", err)
	}
}

func TestACaptureDenialLeavesNoCheckpointBehind(t *testing.T) {
	// A refusal that still published a checkpoint would be worse than no refusal: the
	// caller is told it may not capture, and the artefact exists anyway.
	ws, id, registry := captureWorkspace(t, func(s *policy.Set, id string) {
		s.Revision = "rev-deny-capture"
		theWholeWorkspace(s, id, policy.Deny, authority.WorkspaceCapture)
	})
	if err := os.WriteFile(filepath.Join(ws.Dir(), "report.md"), []byte("work"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{}); err == nil {
		t.Fatal("CreateCheckpoint succeeded; this test is meant to see a refusal")
	}
	// The registry is the only place a capture lands, so an empty checkpoints
	// directory is the observable claim.
	entries, err := os.ReadDir(filepath.Join(registry, "checkpoints"))
	if err != nil {
		if os.IsNotExist(err) {
			return // nothing was published, which is the point
		}
		t.Fatalf("read checkpoints: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a refused capture published %d checkpoint(s) for workspace %s", len(entries), id)
	}
}
