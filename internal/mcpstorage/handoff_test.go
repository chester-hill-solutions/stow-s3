package mcpstorage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestBoundedHandoffBetweenScopedAdapters(t *testing.T) {
	_, source, config := fixture(t)
	config.ExportRoot = t.TempDir()
	config.AdoptRoot = t.TempDir()
	a := &adapter{config: config, adopted: make(map[string]bool)}
	if err := os.WriteFile(filepath.Join(source.Dir(), "progress.txt"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	cp, err := source.CreateCheckpoint(t.Context(), stow.CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	_, out, err := a.export(t.Context(), nil, exportInput{WorkspaceID: source.ID(), CheckpointID: cp.ID, Bundle: "handoff"})
	if err != nil || out.Error != nil {
		t.Fatalf("export: %+v %v", out, err)
	}
	receiverRegistry := t.TempDir()
	receiver, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "receiver"), RegistryDir: receiverRegistry})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	// Use a separate registry, as on a recipient machine.
	receiverConfig := Config{RegistryDir: receiverRegistry, WorkspaceID: receiver.ID(), ExportRoot: config.ExportRoot, AdoptRoot: config.AdoptRoot, MaxBytes: 1 << 20, MaxFiles: 100, Timeout: time.Second * 10}
	b := &adapter{config: receiverConfig, adopted: make(map[string]bool)}
	_, adopted, err := b.adopt(context.Background(), nil, adoptInput{Bundle: "handoff", Destination: "continued"})
	if err != nil || adopted.Error != nil {
		t.Fatalf("adopt: %+v %v", adopted, err)
	}
	body, err := os.ReadFile(filepath.Join(adopted.Workspace.Directory, "progress.txt"))
	if err != nil || string(body) != "continue" {
		t.Fatalf("restored %q %v", body, err)
	}
	if err := b.scope(adopted.Workspace.ID); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffScopeAndPathRefusals(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../escape", "a/b", `a\b`, ".hidden"} {
		if _, err := directChild(t.TempDir(), name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := directChild(root, "escape"); err == nil {
		t.Fatal("symlink accepted")
	}
	_, _, config := fixture(t)
	config.ExportRoot = root
	config.AdoptRoot = t.TempDir()
	bundle := filepath.Join(root, "team")
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	doc := stow.Handoff{Version: 2, WorkspaceID: "other", Team: "escape", Archive: &stow.HandoffArchive{Path: "checkpoint.tar.gz"}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "handoff.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	a := &adapter{config: config}
	_, out, err := a.adopt(t.Context(), nil, adoptInput{Bundle: "team", Destination: "new"})
	if err != nil || out.Error == nil {
		t.Fatalf("team changed scope: %+v %v", out, err)
	}
}
