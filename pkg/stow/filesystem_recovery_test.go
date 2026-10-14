package stow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestFilesystemUnresolvedPendingKeepsObservation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	options := stow.Options{MaxBytes: 5, MaxObjects: 1}
	original := filesystemRuntime(t, dir, options)
	if err := original.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	if _, err := original.SaveObject(ctx, "guarded", "report", []byte("saved"), stow.SaveOptions{Condition: stow.ReplacementCondition(), RequestKey: "retained"}); err != nil {
		t.Fatal(err)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(dir, ".save-pending.json")
	if err := os.WriteFile(pending, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: dir, Options: options})
	if err != nil {
		t.Fatalf("unresolved evidence must preserve observation access: %v", err)
	}
	defer reopened.Close()
	assertUnresolvedFilesystem(t, reopened, pending)
}

func assertUnresolvedFilesystem(t *testing.T, runtime *stow.Runtime, pending string) {
	t.Helper()
	ctx := context.Background()
	object, err := runtime.GetObject(ctx, "guarded", "report")
	if err != nil || string(object.Data) != "saved" || runtime.Usage() != (stow.Usage{Bytes: 5, Objects: 1}) {
		t.Fatalf("read=%+v err=%v usage=%+v", object, err, runtime.Usage())
	}
	receipt, err := runtime.ResolveSave(ctx, "guarded", "report", "retained")
	if err != nil || receipt.Outcome != stow.SaveCommitted {
		t.Fatalf("retained receipt=%+v err=%v", receipt, err)
	}
	if err := runtime.DeleteObject(ctx, "guarded", "report"); !errors.Is(err, stow.ErrSaveRequestUnknown) {
		t.Fatalf("mutation bypassed recovery: %v", err)
	}
	evidence, err := os.ReadFile(pending)
	if err != nil || string(evidence) != "{broken" {
		t.Fatalf("pending evidence=%q err=%v", evidence, err)
	}
}
