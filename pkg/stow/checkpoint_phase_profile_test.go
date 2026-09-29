package stow

import (
	"context"
	"fmt"
	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in diagnostic: go test ./pkg/stow -run TestCheckpointPhaseProfile -v
// with STOW_PROFILE_CHECKPOINT=1. Setup and cleanup are outside phase timings.
func TestCheckpointPhaseProfile(t *testing.T) {
	if os.Getenv("STOW_PROFILE_CHECKPOINT") != "1" {
		t.Skip("set STOW_PROFILE_CHECKPOINT=1")
	}
	registry := filepath.Join(t.TempDir(), "registry")
	w, err := OpenWorkspace(WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "source"), RegistryDir: registry, Bucket: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	data := make([]byte, 16384)
	for i := 0; i < 4096; i++ {
		if err := os.WriteFile(filepath.Join(w.Dir(), fmt.Sprintf("file-%04d", i)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	profileCheckpointPhases(t, captureTarget{dir: w.Dir(), registryDir: registry, workspaceID: w.ID()})
}

func profileCheckpointPhases(t *testing.T, target captureTarget) {
	ctx := context.Background()
	options := CheckpointOptions{PortableObjects: true}
	source, err := rooted.Open(target.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	start := time.Now()
	last := start
	phase := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		t.Logf("%s: %s", name, now.Sub(last))
		last = now
	}
	files, excluded, _, err := checkpointInputs(ctx, source, options)
	phase("file inventory/hash", err)
	portable, err := preparePortableCapture(ctx, target, options, files)
	phase("logical object inventory/hash", err)
	root, stage, err := stageCheckpointDirectory(target.registryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	phase("copy/hash", copyCheckpointFiles(ctx, source, stage, files))
	manifest, err := target.stageManifest(stage, files, excluded, options)
	if err != nil {
		t.Fatal(err)
	}
	phase("logical object verify/hash", finishPortableCapture(ctx, target, stage, portable, &manifest))
	phase("file verify/hash", verifyCheckpointCapture(ctx, source, files, excluded, false))
	phase("durable publication", publishCheckpointContext(ctx, stage, root, manifest.ID, manifest))
	t.Logf("total: %s; files=%d; bytes=%d", time.Since(start), len(files), checkpointManifestBytes(manifest))
}
