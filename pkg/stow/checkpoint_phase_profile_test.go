package stow

import (
	"context"
	"fmt"
	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Opt-in diagnostic: go test ./pkg/stow -run TestCheckpointPhaseProfile -v
// with STOW_PROFILE_CHECKPOINT=1. Setup and cleanup are outside phase timings.
func TestCheckpointPhaseProfile(t *testing.T) {
	if os.Getenv("STOW_PROFILE_CHECKPOINT") != "1" {
		t.Skip("set STOW_PROFILE_CHECKPOINT=1")
	}
	caseName := checkpointProfileCaseName()
	fileCount, fileSize, changedFiles, ok := checkpointProfileShape(caseName)
	if !ok {
		t.Fatalf("unknown STOW_PROFILE_CHECKPOINT_CASE: %s", caseName)
	}
	profileCheckpointRun(t, caseName, fileCount, fileSize, changedFiles)
}

func checkpointProfileCaseName() string {
	if name := os.Getenv("STOW_PROFILE_CHECKPOINT_CASE"); name != "" {
		return name
	}
	return "small-many-files"
}

func profileCheckpointRun(t *testing.T, caseName string, fileCount, fileSize, changedFiles int) {
	t.Helper()
	registry := filepath.Join(t.TempDir(), "registry")
	w := profileWorkspace(t, fileCount, fileSize, registry)
	defer w.Close()
	target := captureTarget{dir: w.Dir(), registryDir: registry, workspaceID: w.ID()}
	parentID := profileParentCheckpoint(t, target, fileSize, changedFiles)
	beforeLogical, beforeAllocated := checkpointProfileTreeBytes(t, filepath.Join(registry, "checkpoints"))
	manifest := profileCheckpointPhases(t, target, CheckpointOptions{ParentID: parentID})
	afterLogical, afterAllocated := checkpointProfileTreeBytes(t, filepath.Join(registry, "checkpoints"))
	inputBytes := int64(fileCount * fileSize)
	verificationTime := profileVerifyCapture(t, w.Dir(), manifest)
	recoveryTime := profileRestoreCheckpoint(t, registry, manifest, inputBytes)
	t.Logf("workload=%s files=%d input_bytes=%d payload_bytes_read_estimate=%d payload_bytes_copied=%d added_persistent_logical_bytes=%d added_persistent_allocated_bytes=%d verification=%s restore=%s",
		caseName, fileCount, inputBytes, inputBytes*3, inputBytes, afterLogical-beforeLogical, afterAllocated-beforeAllocated, verificationTime, recoveryTime)
}

func profileWorkspace(t *testing.T, fileCount, fileSize int, registry string) *Workspace {
	t.Helper()
	w, err := OpenWorkspace(WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "source"), RegistryDir: registry, Bucket: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	data := profilePayload(fileSize)
	for i := 0; i < fileCount; i++ {
		if err := os.WriteFile(filepath.Join(w.Dir(), fmt.Sprintf("file-%04d.bin", i)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func profilePayload(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*31 + 17)
	}
	return data
}

func profileParentCheckpoint(t *testing.T, target captureTarget, fileSize, changedFiles int) string {
	if changedFiles < 0 {
		return ""
	}
	base, err := captureCheckpoint(context.Background(), target, CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if changedFiles > 0 {
		profileChangeFiles(t, target.dir, fileSize, changedFiles)
	}
	return base.ID
}

func profileChangeFiles(t *testing.T, root string, fileSize, changedFiles int) {
	t.Helper()
	changed := profilePayload(fileSize)
	for i := range changed {
		changed[i] = ^changed[i]
	}
	for i := 0; i < changedFiles; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%04d.bin", i)), changed, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func profileVerifyCapture(t *testing.T, root string, manifest CheckpointManifest) time.Duration {
	t.Helper()
	source, err := rooted.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	start := time.Now()
	if err := verifyCheckpointCapture(context.Background(), source, manifest.Files, manifest.Excluded, false); err != nil {
		t.Fatal(err)
	}
	return time.Since(start)
}

func profileRestoreCheckpoint(t *testing.T, registry string, manifest CheckpointManifest, inputBytes int64) time.Duration {
	t.Helper()
	start := time.Now()
	restored, err := RestoreCheckpoint(registry, manifest.ID, WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "restored"), RegistryDir: registry, MaxBytes: inputBytes * 2})
	if err != nil {
		t.Fatal(err)
	}
	_ = restored.Close()
	return time.Since(start)
}

func checkpointProfileShape(name string) (int, int, int, bool) {
	switch name {
	case "small-many-files":
		return 4096, 16 * 1024, -1, true
	case "large-mostly-unchanged":
		return 16, 16 * 1024 * 1024, 0, true
	case "few-changed":
		return 16, 16 * 1024 * 1024, 2, true
	case "large-modified-binary":
		return 1, 128 * 1024 * 1024, 1, true
	default:
		return 0, 0, 0, false
	}
}

func profileCheckpointPhases(t *testing.T, target captureTarget, options CheckpointOptions) CheckpointManifest {
	ctx := context.Background()
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
	if err := finishPortableCapture(ctx, target, stage, portable, &manifest); err != nil {
		t.Fatal(err)
	}
	phase("file verify/hash", verifyCheckpointCapture(ctx, source, files, excluded, false))
	phase("durable publication", publishCheckpointContext(ctx, stage, root, manifest.ID, manifest))
	t.Logf("total: %s; files=%d; bytes=%d", time.Since(start), len(files), checkpointManifestBytes(manifest))
	return manifest
}

func checkpointProfileTreeBytes(t *testing.T, root string) (int64, int64) {
	t.Helper()
	var logical, allocated int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		logical += info.Size()
		stat := reflect.ValueOf(info.Sys())
		if stat.IsValid() && stat.Kind() == reflect.Pointer {
			stat = stat.Elem()
		}
		blocks := reflect.Value{}
		if stat.IsValid() {
			blocks = stat.FieldByName("Blocks")
		}
		if blocks.IsValid() && blocks.CanInt() {
			allocated += blocks.Int() * 512
		} else {
			allocated += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return logical, allocated
}
