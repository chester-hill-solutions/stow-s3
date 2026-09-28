package stow_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

const (
	crashHelperEnv   = "STOW_RESUME_CRASH_HELPER"
	crashRegistryEnv = "STOW_RESUME_CRASH_REGISTRY"
	crashIDEnv       = "STOW_RESUME_CRASH_ID"
	crashReadyEnv    = "STOW_RESUME_CRASH_READY"
)

// TestResumeRecoversAfterHolderIsKilled verifies that the kernel releases a
// workspace's session lock after its owning process is killed, allowing the
// same owned workspace and its stored bytes to be resumed.
func TestResumeRecoversAfterHolderIsKilled(t *testing.T) {
	if os.Getenv(crashHelperEnv) == "1" {
		resumeCrashHelper(t)
		return
	}
	if !workspace.SessionLockSupported() {
		t.Skip("session locks are unsupported on this host")
	}

	ctx := context.Background()
	registry := filepath.Join(t.TempDir(), "registry")
	source := filepath.Join(t.TempDir(), "seed.txt")
	if err := os.WriteFile(source, []byte("prepared"), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	root := filepath.Join(t.TempDir(), "owned-workspace")
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: registry},
		Inputs:           []stow.WorkspaceInput{{Source: source, Destination: "seed.txt"}},
	})
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	const key, value = "output/result.txt", "survives abrupt process death"
	if _, err := prepared.Workspace.PutObject(ctx, prepared.Workspace.Bucket(), key, []byte(value), stow.PutOptions{}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	id := prepared.Workspace.ID()
	if err := prepared.Workspace.Close(); err != nil {
		t.Fatalf("close prepared workspace: %v", err)
	}

	ready := filepath.Join(t.TempDir(), "holder-ready")
	outputPath := filepath.Join(filepath.Dir(ready), "holder.log")
	outputFile, err := os.Create(outputPath)
	if err != nil {
		t.Fatalf("create holder log: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestResumeRecoversAfterHolderIsKilled$")
	cmd.Env = append(os.Environ(),
		crashHelperEnv+"=1",
		crashRegistryEnv+"="+registry,
		crashIDEnv+"="+id,
		crashReadyEnv+"="+ready,
	)
	cmd.Stdout = outputFile
	cmd.Stderr = outputFile
	if err := cmd.Start(); err != nil {
		_ = outputFile.Close()
		t.Fatalf("start holder child: %v", err)
	}
	if err := outputFile.Close(); err != nil {
		t.Fatalf("close holder log: %v", err)
	}
	childDone := false
	t.Cleanup(func() {
		if childDone {
			return
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			childDone = true
			output, _ := os.ReadFile(outputPath)
			t.Fatalf("holder child did not become ready; output: %s", output)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := stow.ResumeIn(registry, id); err == nil {
		t.Fatal("simultaneous ResumeIn succeeded while the child held the session lock")
	} else if !strings.Contains(err.Error(), "in use by a live session") {
		t.Fatalf("simultaneous ResumeIn error = %v, want live-session refusal", err)
	}

	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("kill holder child: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("holder child exited successfully after being killed")
	}
	childDone = true

	resumed, err := stow.ResumeIn(registry, id)
	if err != nil {
		output, _ := os.ReadFile(outputPath)
		t.Fatalf("ResumeIn after child death: %v; child output: %s", err, output)
	}
	defer resumed.Close()
	if resumed.ID() != id {
		t.Fatalf("resumed ID = %q, want %q", resumed.ID(), id)
	}
	object, err := resumed.GetObject(ctx, resumed.Bucket(), key)
	if err != nil {
		t.Fatalf("GetObject after child death: %v", err)
	}
	if string(object.Data) != value {
		t.Fatalf("object after child death = %q, want %q", object.Data, value)
	}
}

func resumeCrashHelper(t *testing.T) {
	registry := os.Getenv(crashRegistryEnv)
	id := os.Getenv(crashIDEnv)
	ready := os.Getenv(crashReadyEnv)
	if registry == "" || id == "" || ready == "" {
		t.Fatal("crash helper is missing its registry, workspace ID, or ready path")
	}
	ws, err := stow.ResumeIn(registry, id)
	if err != nil {
		t.Fatalf("helper ResumeIn: %v", err)
	}
	defer ws.Close()
	if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
		t.Fatalf("write helper ready marker: %v", err)
	}
	for {
		time.Sleep(time.Hour)
	}
}
