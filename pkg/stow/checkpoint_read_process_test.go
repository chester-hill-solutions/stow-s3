package stow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestCheckpointReaderHelperProcess(t *testing.T) {
	if os.Getenv("STOW_CHECKPOINT_READER_HELPER") != "1" {
		return
	}
	reader, err := OpenCheckpointReader(nil, CheckpointReaderOptions{
		RegistryDir:  os.Getenv("STOW_CHECKPOINT_READER_REGISTRY"),
		WorkspaceID:  os.Getenv("STOW_CHECKPOINT_READER_WORKSPACE"),
		CheckpointID: os.Getenv("STOW_CHECKPOINT_READER_CHECKPOINT"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = reader
	if err := os.WriteFile(os.Getenv("STOW_CHECKPOINT_READER_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestCheckpointReaderLeasesReleaseWhenProcessIsKilled(t *testing.T) {
	if !workspace.LockSupported() {
		t.Skip("host advisory locks are unsupported")
	}
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"file.txt": []byte("saved")}, CheckpointOptions{})
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCheckpointReaderHelperProcess$")
	cmd.Env = append(os.Environ(),
		"STOW_CHECKPOINT_READER_HELPER=1",
		"STOW_CHECKPOINT_READER_REGISTRY="+registryDir,
		"STOW_CHECKPOINT_READER_WORKSPACE="+ws.ID(),
		"STOW_CHECKPOINT_READER_CHECKPOINT="+checkpoint.ID,
		"STOW_CHECKPOINT_READER_READY="+ready,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process did not open its checkpoint reader")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed helper process unexpectedly exited successfully")
	}
	cmd.Process = nil
	lease, err := workspace.AcquireCheckpointRemoval(registryDir, checkpoint.ID)
	if err != nil {
		t.Fatalf("checkpoint lock after killed reader: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	registry, err := workspace.OpenRegistry(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RemoveWorkspace(ws.ID(), func() error { return nil }); err != nil {
		t.Fatalf("workspace lock after killed reader: %v", err)
	}
}
