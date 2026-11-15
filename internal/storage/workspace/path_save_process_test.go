//go:build darwin || linux

package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceFileSaveKillHelperProcess(t *testing.T) {
	if os.Getenv("STOW_FILE_SAVE_KILL_HELPER") != "1" {
		return
	}
	root := os.Getenv("STOW_FILE_SAVE_KILL_ROOT")
	phase := os.Getenv("STOW_FILE_SAVE_KILL_PHASE")
	id := os.Getenv("STOW_FILE_SAVE_KILL_ID")
	session, err := AcquireSession(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(Options{Root: root, Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	if store.ID() != id {
		t.Fatal("reopened helper store changed workspace identity")
	}
	newData := []byte("new bytes")
	if phase == "bytes-same" {
		newData = []byte("old")
	}
	request := newWorkspaceFileSaveRequest(t, store, "kill-"+phase, "note.txt", newData)
	if phase == "bytes" || phase == "bytes-same" {
		store.fileSavePayloadWriter = func(path string, data []byte, mode os.FileMode) error {
			if err := store.writeAtomicMode(path, data, mode, false); err != nil {
				return err
			}
			signalSaveKillParent(os.Getenv("STOW_FILE_SAVE_KILL_READY"))
			return nil
		}
	}
	store.fileSavePhaseFault = func(current string) error {
		if current == phase {
			signalSaveKillParent(os.Getenv("STOW_FILE_SAVE_KILL_READY"))
		}
		return nil
	}
	_, _ = store.SaveWorkspaceFile(context.Background(), "bucket", request)
	_ = session
	t.Fatal("save returned before the parent killed the helper")
}

func signalSaveKillParent(path string) {
	_ = os.WriteFile(path, []byte("ready"), 0o600)
	for {
		time.Sleep(time.Hour)
	}
}

func TestWorkspaceFileSaveProcessKillBoundaries(t *testing.T) {
	if !LockSupported() {
		t.Skip("host advisory locks are unsupported")
	}
	for _, scenario := range workspaceFileSaveKillScenarios() {
		t.Run(scenario.phase, func(t *testing.T) { assertWorkspaceFileSaveKillBoundary(t, scenario) })
	}
}

type workspaceFileSaveKillScenario struct {
	phase, outcome string
	wantError      error
	payload        []byte
}

func workspaceFileSaveKillScenarios() []workspaceFileSaveKillScenario {
	return []workspaceFileSaveKillScenario{
		{phase: "prepared", outcome: workspaceFileSaveNotCommitted, wantError: storage.ErrSaveRequestNotCommitted, payload: []byte("old")},
		{phase: "publishing", outcome: string(storage.WorkspaceFileUnknown), wantError: storage.ErrSaveRequestUnknown, payload: []byte("old")},
		{phase: "bytes", outcome: string(storage.WorkspaceFileUnknown), wantError: storage.ErrSaveRequestUnknown, payload: []byte("new bytes")},
		{phase: "bytes-same", outcome: string(storage.WorkspaceFileUnknown), wantError: storage.ErrSaveRequestUnknown, payload: []byte("old")},
		{phase: "published", outcome: workspaceFileSaveCommitted, payload: []byte("new bytes")},
		{phase: "indexed", outcome: workspaceFileSaveCommitted, payload: []byte("new bytes")},
		{phase: "terminal", outcome: workspaceFileSaveCommitted, payload: []byte("new bytes")},
	}
}

func assertWorkspaceFileSaveKillBoundary(t *testing.T, scenario workspaceFileSaveKillScenario) {
	t.Helper()
	root, id := createWorkspaceKillFixture(t)
	ready := filepath.Join(t.TempDir(), "ready")
	output := new(bytes.Buffer)
	cmd := workspaceFileSaveKillCommand(root, id, scenario.phase, ready, output)
	path := filepath.Join(root, "note.txt")
	startAndKillSaveHelper(t, root, cmd, ready, output)
	session, err := AcquireSession(root)
	if err != nil {
		t.Fatalf("session after SIGKILL: %v", err)
	}
	defer session.Release()
	store, err := NewForFileSaveRecovery(Options{Root: root, Bucket: "bucket"}, id)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	resolveKilledSave(t, store, scenario, path, before)
}

func startAndKillSaveHelper(t *testing.T, root string, cmd *exec.Cmd, ready string, output *bytes.Buffer) {
	t.Helper()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	waitForSaveKillSignal(t, cmd, ready, output)
	if _, err := AcquireSession(root); !errors.Is(err, ErrWorkspaceInUse) {
		t.Fatalf("live child session = %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("SIGKILL helper exited successfully")
	}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("helper wait status = %#v, want SIGKILL", cmd.ProcessState.Sys())
	}
	cmd.Process = nil
}

func resolveKilledSave(t *testing.T, store *Store, scenario workspaceFileSaveKillScenario, path string, before os.FileInfo) {
	t.Helper()
	receipt, resolveErr := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", "kill-"+scenario.phase)
	assertKillResolution(t, scenario, receipt, resolveErr)
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, scenario.payload) {
		t.Fatalf("payload=%q error=%v", data, err)
	}
	assertResolvePreservedPayload(t, scenario.phase, path, before)
}

func assertResolvePreservedPayload(t *testing.T, phase, path string, before os.FileInfo) {
	t.Helper()
	if phase != "published" && phase != "indexed" && phase != "terminal" {
		return
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("resolve rewrote payload identity/time: before=%v after=%v error=%v", before, after, err)
	}
}

func createWorkspaceKillFixture(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	store, err := New(Options{Root: root, Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(context.Background(), "bucket", "note.txt", bytes.NewReader([]byte("old")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	id := store.ID()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return root, id
}

func workspaceFileSaveKillCommand(root, id, phase, ready string, output *bytes.Buffer) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestWorkspaceFileSaveKillHelperProcess$")
	command.Stdout, command.Stderr = output, output
	command.Env = append(os.Environ(), "STOW_FILE_SAVE_KILL_HELPER=1", "STOW_FILE_SAVE_KILL_ROOT="+root,
		"STOW_FILE_SAVE_KILL_ID="+id, "STOW_FILE_SAVE_KILL_PHASE="+phase, "STOW_FILE_SAVE_KILL_READY="+ready)
	return command
}

func waitForSaveKillSignal(t *testing.T, cmd *exec.Cmd, ready string, output *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("helper did not reach kill phase: %s", output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertKillResolution(t *testing.T, scenario workspaceFileSaveKillScenario, receipt storage.SaveReceipt, err error) {
	t.Helper()
	if receipt.Outcome != scenario.outcome {
		t.Fatalf("resolution outcome=%q, want %q (error %v)", receipt.Outcome, scenario.outcome, err)
	}
	if scenario.wantError != nil && !errors.Is(err, scenario.wantError) {
		t.Fatalf("resolution error=%v, want %v", err, scenario.wantError)
	}
	if scenario.wantError == nil && err != nil {
		t.Fatalf("resolution error=%v", err)
	}
}

func TestWorkspaceFileSaveTerminalReplayDoesNotRewriteLaterPayload(t *testing.T) {
	root, id := createWorkspaceKillFixture(t)
	store, err := New(Options{Root: root, Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	request := newWorkspaceFileSaveRequest(t, store, "terminal-later", "note.txt", []byte("saved once"))
	receipt, err := store.SaveWorkspaceFile(context.Background(), "bucket", request)
	if err != nil || receipt.Meta == nil {
		t.Fatalf("save=%+v error=%v", receipt, err)
	}
	request.Write.Basis = nil
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("later host bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := AcquireSession(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Release()
	reopened, err := NewForFileSaveRecovery(Options{Root: root, Bucket: "bucket"}, id)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replayed, found, err := reopened.ReplayWorkspaceFileSave(context.Background(), "bucket", request)
	if err != nil || !found || !replayed.Replayed || replayed.Meta == nil || replayed.Meta.VersionID != receipt.Meta.VersionID {
		t.Fatalf("replay=%+v found=%v error=%v", replayed, found, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || !bytes.Equal(data, []byte("later host bytes")) {
		t.Fatalf("later payload=%q error=%v", data, err)
	}
}
