package stow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestDestroyRefusesRestrictedOwnedWorkspaceWithoutChangingIt(t *testing.T) {
	for _, granted := range []stow.Authority{stow.ReadOnly(), stow.ReadWrite(), stow.AllowAll().Without(stow.EnvironmentDestroy)} {
		t.Run(granted.String(), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "owned")
			registry := filepath.Join(t.TempDir(), "registry")
			ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry, Authority: &granted})
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			path := filepath.Join(root, "keep.txt")
			if err := os.WriteFile(path, []byte("retained bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			var refused *stow.ErrNotAuthorized
			if err := ws.Destroy(context.Background()); !errors.As(err, &refused) || refused.Operation != stow.EnvironmentDestroy {
				t.Fatalf("Destroy = %v, want environment.destroy refusal", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "retained bytes" {
				t.Fatalf("refusal changed retained bytes: %q, %v", data, err)
			}
			entries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry})
			if err != nil || len(entries.Workspaces) != 1 || entries.Workspaces[0].ID != ws.ID() {
				t.Fatalf("refusal changed registry entry: %v, %v", entries, err)
			}
			object, err := ws.GetObject(context.Background(), ws.Bucket(), "keep.txt")
			if err != nil || string(object.Data) != "retained bytes" {
				t.Fatalf("refusal closed handle: %q, %v", object.Data, err)
			}
		})
	}
}

func TestDestroyConsultsResolvedAuthorityAfterClose(t *testing.T) {
	granted := stow.ReadWrite()
	root := filepath.Join(t.TempDir(), "owned")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: filepath.Join(t.TempDir(), "registry"), Authority: &granted})
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	granted = granted.With(stow.EnvironmentDestroy)
	var refused *stow.ErrNotAuthorized
	if err := ws.Destroy(context.Background()); !errors.As(err, &refused) || refused.Operation != stow.EnvironmentDestroy {
		t.Fatalf("Destroy after Close = %v, want resolved authority refusal", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("refusal removed closed workspace: %v", err)
	}
}

func TestExplicitDestroyGrantDoesNotRequireObjectWrite(t *testing.T) {
	granted := stow.ReadOnly().With(stow.EnvironmentDestroy)
	root := filepath.Join(t.TempDir(), "owned")
	registry := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: registry, Authority: &granted})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for range 2 {
		if err := ws.Destroy(context.Background()); err != nil {
			t.Fatalf("authorized Destroy: %v", err)
		}
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authorized Destroy retained workspace: %v", err)
	}
	entries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry})
	if err != nil || len(entries.Workspaces) != 0 {
		t.Fatalf("authorized Destroy retained registry entry: %v, %v", entries, err)
	}
}

func TestPrepareRollbackCleansRestrictedStagingWithoutGrantingDestroy(t *testing.T) {
	for _, granted := range []stow.Authority{stow.ReadOnly(), stow.ReadWrite()} {
		t.Run(granted.String(), func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "input.txt")
			if err := os.WriteFile(source, []byte("input bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(t.TempDir(), "owned")
			registry := filepath.Join(t.TempDir(), "registry")
			options := stow.PrepareOptions{
				WorkspaceOptions: stow.WorkspaceOptions{Dir: root, RegistryDir: registry, Authority: &granted},
				Inputs:           []stow.WorkspaceInput{{Source: source, Destination: "repo/../escape"}},
			}
			if _, err := stow.PrepareWorkspace(options); err == nil {
				t.Fatal("invalid prepare succeeded")
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("constructor rollback retained staging: %v", err)
			}
			entries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: registry})
			if err != nil || len(entries.Workspaces) != 0 {
				t.Fatalf("constructor rollback retained registry entry: %v, %v", entries, err)
			}
			options.Inputs[0].Destination = "input.txt"
			prepared, err := stow.PrepareWorkspace(options)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Workspace.Close()
			var refused *stow.ErrNotAuthorized
			if err := prepared.Workspace.Destroy(context.Background()); !errors.As(err, &refused) {
				t.Fatalf("committed restricted workspace Destroy = %v, want refusal", err)
			}
			data, err := os.ReadFile(filepath.Join(root, "input.txt"))
			if err != nil || string(data) != "input bytes" {
				t.Fatalf("committed bytes changed: %q, %v", data, err)
			}
		})
	}
}
