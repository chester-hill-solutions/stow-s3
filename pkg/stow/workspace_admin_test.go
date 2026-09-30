package stow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func administrativeWorkspace(t *testing.T, root, registry string, granted stow.Authority) *stow.Workspace {
	t.Helper()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry, Authority: &granted})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func registryEntryBytes(t *testing.T, registry, id string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(registry, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertAdministrativeRefusalUnchanged(t *testing.T, ws *stow.Workspace, registry string, before []byte) {
	t.Helper()
	if !bytes.Equal(before, registryEntryBytes(t, registry, ws.ID())) {
		t.Fatal("administrative refusal changed registry identity or actor policy")
	}
	if _, err := os.Stat(ws.Dir()); err != nil {
		t.Fatalf("administrative refusal removed workspace: %v", err)
	}
}

func TestAdministrativeDestroyRefusesLiveRestrictedActorAndDeletesAfterClose(t *testing.T) {
	for _, granted := range []stow.Authority{stow.ReadOnly(), stow.ReadWrite()} {
		t.Run(granted.String(), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "owned")
			registry := filepath.Join(t.TempDir(), "registry")
			ws := administrativeWorkspace(t, root, registry, granted)
			before := registryEntryBytes(t, registry, ws.ID())
			if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, ws.ID()); !errors.Is(err, workspace.ErrWorkspaceInUse) {
				t.Fatalf("live administrative destroy = %v, want workspace in use", err)
			}
			assertAdministrativeRefusalUnchanged(t, ws, registry, before)
			if ws.Authority() != granted {
				t.Fatal("administrative operation widened issued actor grant")
			}
			if err := ws.Close(); err != nil {
				t.Fatal(err)
			}
			if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, ws.ID()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("closed administrative destroy retained root: %v", err)
			}
			if _, err := os.Stat(filepath.Join(registry, ws.ID()+".json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("administrative destroy retained registry entry: %v", err)
			}
		})
	}
}

func TestAdministrativeDestroyRefusesAdoptedDirectoryWithoutPolicyChange(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws := administrativeWorkspace(t, t.TempDir(), registry, stow.ReadOnly())
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	before := registryEntryBytes(t, registry, ws.ID())
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, ws.ID()); !errors.Is(err, workspace.ErrNotDestructible) {
		t.Fatalf("adopted administrative destroy = %v, want ownership refusal", err)
	}
	assertAdministrativeRefusalUnchanged(t, ws, registry, before)
}

func TestAdministrativeDestroyRefusesProtectedDirectoryWithoutPolicyChange(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws := administrativeWorkspace(t, filepath.Join(t.TempDir(), "owned"), registry, stow.ReadWrite())
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	t.Chdir(ws.Dir())
	before := registryEntryBytes(t, registry, ws.ID())
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, ws.ID()); !errors.Is(err, workspace.ErrNotDestructible) {
		t.Fatalf("protected administrative destroy = %v, want protected-path refusal", err)
	}
	assertAdministrativeRefusalUnchanged(t, ws, registry, before)
}

func TestAdministrativeDestroyRefusesMismatchedWorkspaceIdentity(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	first := administrativeWorkspace(t, filepath.Join(t.TempDir(), "first"), registry, stow.ReadOnly())
	second := administrativeWorkspace(t, filepath.Join(t.TempDir(), "second"), registry, stow.ReadWrite())
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	var entry workspace.Entry
	if err := json.Unmarshal(registryEntryBytes(t, registry, first.ID()), &entry); err != nil {
		t.Fatal(err)
	}
	entry.Dir = second.Dir()
	modified, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, first.ID()+".json"), modified, 0o600); err != nil {
		t.Fatal(err)
	}
	secondBefore := registryEntryBytes(t, registry, second.ID())
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, first.ID()); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched administrative destroy = %v, want identity refusal", err)
	}
	assertAdministrativeRefusalUnchanged(t, first, registry, modified)
	assertAdministrativeRefusalUnchanged(t, second, registry, secondBefore)
}

func TestAdministrativeDestroyHonorsCancellationCaptureAndCheckpointCleanup(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws := administrativeWorkspace(t, filepath.Join(t.TempDir(), "owned"), registry, stow.ReadWrite())
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	before := registryEntryBytes(t, registry, ws.ID())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := stow.DestroyRegisteredWorkspace(ctx, registry, ws.ID()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled administrative destroy = %v", err)
	}
	assertAdministrativeRefusalUnchanged(t, ws, registry, before)
	lock, err := workspace.AcquireCapture(registry, ws.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, ws.ID()); !errors.Is(err, workspace.ErrCaptureInProgress) {
		t.Fatalf("capture-contended administrative destroy = %v", err)
	}
	assertAdministrativeRefusalUnchanged(t, ws, registry, before)
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := stow.DestroyRegisteredWorkspace(context.Background(), registry, ws.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := stow.LoadCheckpoint(registry, checkpoint.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("administrative destruction retained checkpoint: %v", err)
	}
}
