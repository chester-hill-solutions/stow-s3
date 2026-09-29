//go:build darwin || linux

package mcpstorage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestSpecialHandoffReferenceIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := stow.ReadHandoff(path); err == nil {
		t.Fatal("FIFO reference accepted")
	}
}

func TestMCPRefusesSpecialArchiveBeforeImport(t *testing.T) {
	_, _, config := fixture(t)
	config.ExportRoot, config.AdoptRoot = t.TempDir(), t.TempDir()
	bundle := filepath.Join(config.ExportRoot, "bundle")
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "handoff.json"), []byte(`{"version":2,"workspace_id":"source","archive":{"path":"checkpoint.tar.gz","sha256":"digest"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(bundle, "checkpoint.tar.gz"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &adapter{config: config}
	_, out, err := a.adopt(t.Context(), nil, adoptInput{Bundle: "bundle", Destination: "new"})
	if err != nil || out.Error == nil {
		t.Fatalf("accepted FIFO: %+v %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(config.AdoptRoot, "new")); !os.IsNotExist(err) {
		t.Fatalf("destination created: %v", err)
	}
}
