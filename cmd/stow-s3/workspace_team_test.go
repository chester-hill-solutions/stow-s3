package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// A runner that serves two jobs must be able to say which team a workspace
// belongs to, and the answer has to reach the reference a handoff hands to the
// next machine. This is the whole path: a manifest names the team, the workspace
// is filed under it, and the handoff says so.
func TestWorkspacePrepareAndHandoffCarryTheTeam(t *testing.T) {
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	if err := os.Mkdir(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "TASK.md"), []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(base, "task.json")
	manifest := `{"version":1,"root":"` + filepath.Join(base, "ws") + `","registry_dir":"` + base + `","team":"platform","inputs":[{"source":"` + seed + `","destination":"seed"}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureWorkspaceCommand(t, func() error {
		return prepareWorkspaceCommand([]string{"--manifest", manifestPath})
	})
	var prepared workspaceResult
	if err := json.Unmarshal(output, &prepared); err != nil {
		t.Fatalf("prepare response = %s: %v", output, err)
	}
	if prepared.Team != "platform" {
		t.Fatalf("prepared team = %q, want platform", prepared.Team)
	}
	entry := filepath.Join(base, "teams", "platform", prepared.WorkspaceID+".json")
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("the entry is not filed under the team partition: %v", err)
	}

	// The reference a handoff hands to another machine has to name the partition
	// too, or the receiver files it under whatever its own root holds.
	handoffPath := filepath.Join(base, "handoff.json")
	if err := handoffWorkspaceCommand([]string{"--id", prepared.WorkspaceID, "--registry-dir", base, "--team", "platform", "--output", handoffPath}); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	document := readHandoff(t, handoffPath)
	if document.Team != "platform" {
		t.Fatalf("handoff team = %q, want platform", document.Team)
	}
	if document.RegistryDir != filepath.Join(base, "teams", "platform") {
		t.Fatalf("handoff registry = %q, want the partition it resolved to", document.RegistryDir)
	}

	// And resume follows the reference rather than the root, which is why the
	// registry it records is already the team's.
	output = captureWorkspaceCommand(t, func() error {
		return resumeWorkspaceCommand([]string{"--handoff", handoffPath})
	})
	var resumed workspaceResult
	if err := json.Unmarshal(output, &resumed); err != nil {
		t.Fatalf("resume response = %s: %v", output, err)
	}
	if resumed.WorkspaceID != prepared.WorkspaceID {
		t.Fatalf("resume = %s, want the handed-off workspace", resumed.WorkspaceID)
	}
	if _, err := os.Stat(filepath.Join(base, resumed.WorkspaceID+".json")); err == nil {
		t.Fatal("resuming from a handoff registered the workspace at the registry root")
	}
}

// Every registry-scoped verb has to be able to name a team, or the partition is
// a feature that exists only where somebody remembered to add it. This walks the
// ones that touch a workspace or its checkpoints, given the root rather than the
// partition, because spelling `teams/<name>` into a path is exactly what the
// caller should not have to do.
func TestEveryRegistryScopedVerbAcceptsATeam(t *testing.T) {
	base, prepared := prepareInTeam(t, "platform")
	teamArgs := []string{"--registry-dir", base, "--team", "platform"}

	output := captureWorkspaceCommand(t, func() error {
		return checkpointWorkspaceCommand(append([]string{"--id", prepared.WorkspaceID}, teamArgs...))
	})
	var checkpoint checkpointResult
	if err := json.Unmarshal(output, &checkpoint); err != nil {
		t.Fatalf("checkpoint response = %s: %v", output, err)
	}
	archive := filepath.Join(base, "cp.tar.gz")
	deltaPath := filepath.Join(base, "change.stowdelta")
	// Each verb answers with a different shape, so each is checked for the field
	// that proves it found the checkpoint in the team and not at the root.
	for name, run := range map[string]func() error{
		"export": func() error {
			return exportCheckpointCommand(append([]string{"--checkpoint-id", checkpoint.ID, "--output", archive}, teamArgs...))
		},
		"delta": func() error {
			return deltaWorkspaceCommand(append([]string{"--from", checkpoint.ID, "--to", checkpoint.ID, "--output", deltaPath}, teamArgs...))
		},
		"diff": func() error {
			return diffWorkspaceCommand(append([]string{"--from", checkpoint.ID, "--to", checkpoint.ID}, teamArgs...))
		},
		"restore": func() error {
			return restoreCheckpointCommand(append([]string{"--checkpoint-id", checkpoint.ID, "--root", filepath.Join(base, "restored")}, teamArgs...))
		},
	} {
		out := captureWorkspaceCommand(t, run)
		var answered map[string]json.RawMessage
		if err := json.Unmarshal(out, &answered); err != nil {
			t.Errorf("%s response = %s: %v", name, out, err)
			continue
		}
		if name == "diff" {
			if _, ok := answered["changes"]; !ok {
				t.Errorf("diff response = %s, want a change list", out)
			}
			continue
		}
		if name == "restore" {
			if _, ok := answered["workspace_id"]; !ok {
				t.Errorf("restore response = %s, want a workspace id", out)
			}
			continue
		}
		if !strings.Contains(string(out), checkpoint.ID) {
			t.Errorf("%s response = %s, want it to name the checkpoint", name, out)
		}
	}
	// And without the team, the same ID is not in the root registry.
	if err := checkpointWorkspaceCommand([]string{"--id", prepared.WorkspaceID, "--registry-dir", base}); err == nil {
		t.Error("a team workspace was checkpointed from the registry root")
	}
}

// prepareInTeam prepares one workspace in a named team and returns the base
// directory and the launch descriptor.
func prepareInTeam(t *testing.T, team string) (string, workspaceResult) {
	t.Helper()
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	if err := os.Mkdir(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "TASK.md"), []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(base, "task.json")
	manifest := `{"version":1,"root":"` + filepath.Join(base, "ws") + `","registry_dir":"` + base + `","team":"` + team + `","inputs":[{"source":"` + seed + `","destination":"work"}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureWorkspaceCommand(t, func() error {
		return prepareWorkspaceCommand([]string{"--manifest", manifestPath})
	})
	var prepared workspaceResult
	if err := json.Unmarshal(output, &prepared); err != nil {
		t.Fatalf("prepare response = %s: %v", output, err)
	}
	return base, prepared
}

// A team name that cannot be a directory is refused at the boundary, where the
// caller can see what they asked for, rather than becoming a workspace filed
// somewhere unexpected.
func TestWorkspacePrepareRefusesAnUnusableTeamName(t *testing.T) {
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	if err := os.Mkdir(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "TASK.md"), []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(base, "task.json")
	manifest := `{"version":1,"root":"` + filepath.Join(base, "ws") + `","registry_dir":"` + base + `","team":"../escape","inputs":[{"source":"` + seed + `","destination":"seed"}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	err := prepareWorkspaceCommand([]string{"--manifest", manifestPath})
	if err == nil {
		t.Fatal("a manifest with a climbing team name was prepared")
	}
	if !strings.Contains(err.Error(), "team") {
		t.Fatalf("error = %v, want it to name the team", err)
	}
}

// Adopting into a team the document disagrees with is two intents, and the
// receiving machine is not going to pick one.
func TestWorkspaceAdoptRefusesATeamTheHandoffDoesNotName(t *testing.T) {
	base := t.TempDir()
	document := workspaceHandoff{
		Version: handoffLocalVersion, WorkspaceID: "ws_1", RegistryDir: base, Team: "platform",
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	handoffPath := filepath.Join(base, "handoff.json")
	if err := os.WriteFile(handoffPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	err = adoptHandoffCommand([]string{"--handoff", handoffPath, "--root", filepath.Join(base, "adopted"), "--team", "product"})
	if err == nil {
		t.Fatal("a handoff was adopted under a team it does not name")
	}
	if !strings.Contains(err.Error(), "team") {
		t.Fatalf("error = %v, want it to name the disagreement", err)
	}
}

// The prepare result reports the partition it filed into, so a caller never has
// to reconstruct where its workspace went.
func TestWorkspacePrepareResultNamesItsPartition(t *testing.T) {
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	if err := os.Mkdir(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(base, "task.json")
	manifest := `{"version":1,"root":"` + filepath.Join(base, "ws") + `","registry_dir":"` + base + `","inputs":[{"source":"` + seed + `","destination":"seed"}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureWorkspaceCommand(t, func() error {
		return prepareWorkspaceCommand([]string{"--manifest", manifestPath})
	})
	var prepared workspaceResult
	if err := json.Unmarshal(output, &prepared); err != nil {
		t.Fatalf("prepare response = %s: %v", output, err)
	}
	if prepared.RegistryDir != base {
		t.Fatalf("prepare reported registry %q, want the root it was given", prepared.RegistryDir)
	}
	if prepared.Team != "" {
		t.Fatalf("prepare reported team %q, want none", prepared.Team)
	}
	// A relative registry directory is made absolute before it is reported, because
	// the next process reads it from a different directory.
	relative, err := stow.ResolveRegistryDir(".", "")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(relative) {
		t.Fatalf("ResolveRegistryDir returned a relative path %q", relative)
	}
}
