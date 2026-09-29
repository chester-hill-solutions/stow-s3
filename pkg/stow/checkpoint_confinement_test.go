package stow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

func TestCheckpointCopyRefusesAncestorReplacement(t *testing.T) {
	path, outside := t.TempDir(), t.TempDir()
	directory := filepath.Join(path, "nested")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "file"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := rooted.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	files, _, _, err := scanCheckpointRoot(context.Background(), source, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(directory, filepath.Join(path, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, directory); err != nil {
		t.Skip(err)
	}
	if err := copyCheckpointFiles(context.Background(), source, t.TempDir(), files); err == nil {
		t.Fatal("copied through replaced ancestor")
	}
}

func TestCheckpointCopyWorkersCompleteBeforeReturning(t *testing.T) {
	path, stage := t.TempDir(), t.TempDir()
	for index := range 64 {
		if err := os.WriteFile(filepath.Join(path, fmt.Sprint(index)), []byte(fmt.Sprint(index)), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	source, err := rooted.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	files, _, _, err := scanCheckpointRoot(context.Background(), source, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyCheckpointFiles(context.Background(), source, stage, files); err != nil {
		t.Fatal(err)
	}
	manifest := CheckpointManifest{Version: 1, Files: files}
	if _, err := verifyCheckpointReceiptPayload(context.Background(), stage, manifest); err != nil {
		t.Fatal("copy returned with incomplete payloads:", err)
	}
}

func TestCheckpointCopyCancellationBeforeAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stage := t.TempDir()
	if err := copyCheckpointFiles(ctx, nil, stage, []CheckpointFile{{Path: "file"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("copy error = %v", err)
	}
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled copy staged files: %v, %v", entries, err)
	}
}

func TestCheckpointCopyBoundsGrowingFile(t *testing.T) {
	path, stage := t.TempDir(), t.TempDir()
	file := filepath.Join(path, "file")
	if err := os.WriteFile(file, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := rooted.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	files, _, _, err := scanCheckpointRoot(context.Background(), source, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, make([]byte, 1024*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyCheckpointFiles(context.Background(), source, stage, files); err == nil {
		t.Fatal("accepted growing input")
	}
	info, err := os.Stat(filepath.Join(stage, "files", "file"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 2 {
		t.Fatalf("unbounded staged bytes: %d", info.Size())
	}
}
