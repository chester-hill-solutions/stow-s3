package stow_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestHandoffBundleMovesWithoutEditing(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "source"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := os.WriteFile(filepath.Join(ws.Dir(), "progress.md"), []byte("continue here"), 0600); err != nil {
		t.Fatal(err)
	}
	cp, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "bundle")
	document, err := stow.ExportHandoff(context.Background(), stow.HandoffExportOptions{RegistryDir: registry, WorkspaceID: ws.ID(), CheckpointID: cp.ID, BundleDir: bundle})
	if err != nil {
		t.Fatal(err)
	}
	if document.Archive.Path != "checkpoint.tar.gz" {
		t.Fatalf("archive: %+v", document.Archive)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(bundle, moved); err != nil {
		t.Fatal(err)
	}
	adopted, imported, err := stow.AdoptHandoff(context.Background(), filepath.Join(moved, "handoff.json"), stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "destination"), RegistryDir: t.TempDir()}, stow.CheckpointArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer adopted.Close()
	data, err := os.ReadFile(filepath.Join(adopted.Dir(), "progress.md"))
	if err != nil || string(data) != "continue here" {
		t.Fatalf("restored = %q, %v", data, err)
	}
	if imported.ID != cp.ID || adopted.ID() == ws.ID() {
		t.Fatal("lineage identity changed")
	}
	document.WorkspaceID = "different"
	newRegistry := filepath.Join(t.TempDir(), "registry")
	if _, err := stow.ImportHandoff(context.Background(), filepath.Join(moved, "handoff.json"), newRegistry, document, stow.CheckpointArchiveOptions{}); err == nil {
		t.Fatal("mismatched identity accepted")
	}
	if _, err := os.Stat(newRegistry); !os.IsNotExist(err) {
		t.Fatalf("mismatched archive created registry: %v", err)
	}
}

func TestHandoffExportRejectsWrongWorkspaceBeforeWriting(t *testing.T) {
	registry := t.TempDir()
	first, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "first"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "second"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	cp, err := first.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "bundle")
	_, err = stow.ExportHandoff(context.Background(), stow.HandoffExportOptions{RegistryDir: registry, WorkspaceID: second.ID(), CheckpointID: cp.ID, BundleDir: bundle})
	if err == nil {
		t.Fatal("accepted foreign checkpoint")
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatalf("created bundle: %v", err)
	}
}

func TestAdoptOpenedArchiveDoesNotReopenChangedBundle(t *testing.T) {
	bundle, document := openedHandoffFixture(t)
	archive, err := os.Open(filepath.Join(bundle, "checkpoint.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if err := os.Rename(bundle, bundle+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "handoff.json"), []byte(`{"team":"outside"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "checkpoint.tar.gz"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := t.TempDir()
	ws, cp, err := stow.AdoptHandoffArchive(t.Context(), document, archive, stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "restored"), RegistryDir: registry}, stow.CheckpointArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if cp.ID != document.CheckpointID {
		t.Fatal("imported changed identity")
	}
	if _, err := stow.LookupWorkspace(registry, ws.ID()); err != nil {
		t.Fatal(err)
	}
}

func openedHandoffFixture(t *testing.T) (string, stow.Handoff) {
	t.Helper()
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "source"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	cp, err := ws.CreateCheckpoint(t.Context(), stow.CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "bundle")
	document, err := stow.ExportHandoff(t.Context(), stow.HandoffExportOptions{RegistryDir: registry, WorkspaceID: ws.ID(), CheckpointID: cp.ID, BundleDir: bundle})
	if err != nil {
		t.Fatal(err)
	}
	return bundle, document
}
