package stow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

func TestPrepareRejectsOversizedInputBeforeDestinationMutation(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(source, []byte("too large"), 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := seedPreparedWorkspace(root, PrepareOptions{
		WorkspaceOptions: WorkspaceOptions{MaxBytes: 1},
		Inputs:           []WorkspaceInput{{Source: source, Destination: "blocked/file"}},
	})
	if err == nil || !strings.Contains(err.Error(), "workspace limit") {
		t.Fatalf("expected admission before opening destination: %v", err)
	}
	data, readErr := os.ReadFile(blocked)
	if readErr != nil || string(data) != "unchanged" {
		t.Fatalf("changed destination: %q %v", data, readErr)
	}
}

func TestPrepareSharesBudgetAcrossInputs(t *testing.T) {
	for _, options := range []WorkspaceOptions{{MaxBytes: 5}, {MaxObjects: 1}} {
		root, source := t.TempDir(), t.TempDir()
		for _, name := range []string{"a", "b"} {
			if err := os.WriteFile(filepath.Join(source, name), []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		_, _, _, err := seedPreparedWorkspace(root, PrepareOptions{WorkspaceOptions: options, Inputs: []WorkspaceInput{{Source: source}}})
		if err == nil || !strings.Contains(err.Error(), "workspace limit") {
			t.Fatalf("budget not enforced: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "b")); !os.IsNotExist(err) {
			t.Fatalf("over-budget file was created: %v", err)
		}
	}
}

func TestCheckpointAdmissionStopsBeforeLaterInvalidInputs(t *testing.T) {
	for _, options := range []CheckpointOptions{{MaxBytes: 1}, {MaxFiles: 1}} {
		ws, err := OpenWorkspace(WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ws.Close() })
		for _, name := range []string{"a", "b"} {
			if err := os.WriteFile(filepath.Join(ws.Dir(), name), []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink("a", filepath.Join(ws.Dir(), "z-link")); err != nil {
			t.Fatal(err)
		}
		_, err = ws.CreateCheckpoint(context.Background(), options)
		var failure *CheckpointError
		if !errors.As(err, &failure) || failure.Code != "capacity_exceeded" {
			t.Fatalf("late admission: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(ws.registryDir, "checkpoints"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("failed capture published checkpoints: %v", entries)
		}
	}
}

func TestPrepareRechecksInputSizeBeforeCopy(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(source, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("grown"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "copy")
	if err := copySeedFile(source, destination, before, &seedContext{maxBytes: 1}); err == nil {
		t.Fatal("accepted input that grew beyond the budget")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("oversized input was copied: %v", err)
	}
}

func TestPrepareAdmitsGitTreeBeforeCheckout(t *testing.T) {
	source := budgetGitSource(t)
	root := t.TempDir()
	_, _, _, err := seedPreparedWorkspace(root, PrepareOptions{
		WorkspaceOptions: WorkspaceOptions{MaxBytes: 1},
		Repositories:     []GitRepositoryInput{{Source: source, Destination: "repo", Ref: "HEAD"}},
	})
	if err == nil || !strings.Contains(err.Error(), "workspace limit") {
		t.Fatalf("Git budget not enforced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "repo")); !os.IsNotExist(err) {
		t.Fatalf("over-budget checkout was created: %v", err)
	}
}

func TestPrepareSharesBudgetBetweenGitAndFiles(t *testing.T) {
	source := budgetGitSource(t)
	options := PrepareOptions{Repositories: []GitRepositoryInput{{Source: source, Destination: "repo", Ref: "HEAD"}}}
	_, objects, _, err := seedPreparedWorkspace(t.TempDir(), options)
	if err != nil {
		t.Fatal(err)
	}
	options.MaxObjects = objects
	options.Inputs = []WorkspaceInput{{Source: filepath.Join(source, "file"), Destination: "extra"}}
	root := t.TempDir()
	_, _, _, err = seedPreparedWorkspace(root, options)
	if err == nil || !strings.Contains(err.Error(), "workspace limit") {
		t.Fatalf("Git and files did not share a budget: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "extra")); !os.IsNotExist(err) {
		t.Fatalf("over-budget extra file was created: %v", err)
	}
}

func TestPrepareAdmitsGitMetadataBeforeCheckout(t *testing.T) {
	source := budgetGitSource(t)
	for _, limits := range []WorkspaceOptions{{MaxBytes: 7}, {MaxObjects: 1}} {
		root := t.TempDir()
		_, _, _, err := seedPreparedWorkspace(root, PrepareOptions{
			WorkspaceOptions: limits,
			Repositories:     []GitRepositoryInput{{Source: source, Destination: "repo", Ref: "HEAD"}},
		})
		if err == nil || !strings.Contains(err.Error(), "workspace limit") {
			t.Fatalf("Git metadata budget not enforced: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "repo", "file")); !os.IsNotExist(err) {
			t.Fatalf("working files were checked out before metadata admission: %v", err)
		}
	}
}

func budgetGitSource(t *testing.T) string {
	t.Helper()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--quiet"}, {"add", "file"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "seed"},
	} {
		if _, err := gitCommand(source, args...); err != nil {
			t.Fatal(err)
		}
	}
	return source
}

func TestCheckpointVerificationRejectsGrowthBeforeLaterInputs(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "a")
	if err := os.WriteFile(path, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := rooted.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files, excluded, _, err := scanCheckpointRoot(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("grown"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(directory, "z-link")); err != nil {
		t.Fatal(err)
	}
	err = verifyCheckpointCapture(context.Background(), root, files, excluded, false)
	var failure *CheckpointError
	if !errors.As(err, &failure) || failure.Code != "workspace_changed" {
		t.Fatalf("verification scanned beyond its inventory budget: %v", err)
	}
}
