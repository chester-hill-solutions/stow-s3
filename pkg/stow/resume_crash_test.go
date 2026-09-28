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
	crashObjectKey   = "output/result.txt"
	crashObjectValue = "survives abrupt process death"
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

	registry, id := prepareCrashRecoveryWorkspace(t)
	ready := filepath.Join(t.TempDir(), "holder-ready")
	holder := startCrashHolder(t, registry, id, ready)
	waitForCrashHolder(t, holder, ready)
	assertCrashHolderOwnsSession(t, registry, id)
	killCrashHolder(t, holder)
	assertWorkspaceResumesAfterCrash(t, holder, registry, id)
}

type crashHolder struct {
	command    *exec.Cmd
	outputPath string
	done       bool
}

func prepareCrashRecoveryWorkspace(t *testing.T) (registry, id string) {
	t.Helper()
	registry = filepath.Join(t.TempDir(), "registry")
	source := filepath.Join(t.TempDir(), "seed.txt")
	if err := os.WriteFile(source, []byte("prepared"), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{
		WorkspaceOptions: stow.WorkspaceOptions{
			Dir: filepath.Join(t.TempDir(), "owned-workspace"), RegistryDir: registry,
		},
		Inputs: []stow.WorkspaceInput{{Source: source, Destination: "seed.txt"}},
	})
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	if _, err := prepared.Workspace.PutObject(context.Background(), prepared.Workspace.Bucket(), crashObjectKey, []byte(crashObjectValue), stow.PutOptions{}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	id = prepared.Workspace.ID()
	if err := prepared.Workspace.Close(); err != nil {
		t.Fatalf("close prepared workspace: %v", err)
	}
	return registry, id
}

func startCrashHolder(t *testing.T, registry, id, ready string) *crashHolder {
	t.Helper()
	outputPath := filepath.Join(filepath.Dir(ready), "holder.log")
	outputFile, err := os.Create(outputPath)
	if err != nil {
		t.Fatalf("create holder log: %v", err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestResumeRecoversAfterHolderIsKilled$")
	command.Env = append(os.Environ(),
		crashHelperEnv+"=1",
		crashRegistryEnv+"="+registry,
		crashIDEnv+"="+id,
		crashReadyEnv+"="+ready,
	)
	command.Stdout = outputFile
	command.Stderr = outputFile
	if err := command.Start(); err != nil {
		_ = outputFile.Close()
		t.Fatalf("start holder child: %v", err)
	}
	if err := outputFile.Close(); err != nil {
		t.Fatalf("close holder log: %v", err)
	}
	holder := &crashHolder{command: command, outputPath: outputPath}
	t.Cleanup(func() {
		if !holder.done {
			_ = holder.command.Process.Kill()
			_ = holder.command.Wait()
		}
	})
	return holder
}

func waitForCrashHolder(t *testing.T, holder *crashHolder, ready string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return
		}
		if time.Now().After(deadline) {
			_ = holder.command.Process.Kill()
			_ = holder.command.Wait()
			holder.done = true
			output, _ := os.ReadFile(holder.outputPath)
			t.Fatalf("holder child did not become ready; output: %s", output)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertCrashHolderOwnsSession(t *testing.T, registry, id string) {
	t.Helper()
	if _, err := stow.ResumeIn(registry, id); err == nil {
		t.Fatal("simultaneous ResumeIn succeeded while the child held the session lock")
	} else if !strings.Contains(err.Error(), "in use by a live session") {
		t.Fatalf("simultaneous ResumeIn error = %v, want live-session refusal", err)
	}
}

func killCrashHolder(t *testing.T, holder *crashHolder) {
	t.Helper()
	if err := holder.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("kill holder child: %v", err)
	}
	if err := holder.command.Wait(); err == nil {
		t.Fatal("holder child exited successfully after being killed")
	}
	holder.done = true
}

func assertWorkspaceResumesAfterCrash(t *testing.T, holder *crashHolder, registry, id string) {
	t.Helper()
	resumed, err := stow.ResumeIn(registry, id)
	if err != nil {
		output, _ := os.ReadFile(holder.outputPath)
		t.Fatalf("ResumeIn after child death: %v; child output: %s", err, output)
	}
	defer resumed.Close()
	if resumed.ID() != id {
		t.Fatalf("resumed ID = %q, want %q", resumed.ID(), id)
	}
	object, err := resumed.GetObject(context.Background(), resumed.Bucket(), crashObjectKey)
	if err != nil {
		t.Fatalf("GetObject after child death: %v", err)
	}
	if string(object.Data) != crashObjectValue {
		t.Fatalf("object after child death = %q, want %q", object.Data, crashObjectValue)
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
