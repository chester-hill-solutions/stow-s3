package stow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHandoffParentSyncFailureRetainsPublishedBundle(t *testing.T) {
	parent := t.TempDir()
	stage, destination := filepath.Join(parent, "stage"), filepath.Join(parent, "bundle")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "handoff.json"), []byte("published"), 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("sync failure")
	err := commitHandoff(stage, destination, func(string) error { return failure })
	var unknown *HandoffError
	if !errors.As(err, &unknown) || !errors.Is(err, failure) {
		t.Fatalf("untyped error: %v", err)
	}
	if unknown.Outcome != "unknown" || unknown.BundleDir != destination {
		t.Fatalf("wrong outcome: %+v", unknown)
	}
	data, err := os.ReadFile(filepath.Join(destination, "handoff.json"))
	if err != nil || string(data) != "published" {
		t.Fatalf("published bundle missing: %q %v", data, err)
	}
}
