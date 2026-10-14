package fs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func interruptedRuntimeStore(t *testing.T) *FilesystemStore {
	t.Helper()
	store := saveStore(t, t.TempDir())
	initializeSaveBucket(t, store)
	ctx := context.Background()
	if _, err := store.PutObject(ctx, "saved", "known", bytes.NewBufferString("saved"), storage.PutOptions{RequestKey: "known-request"}); err != nil {
		t.Fatal(err)
	}
	store.saveFault = func(phase string) error {
		if phase == "publishing" {
			return errors.New("interrupted publication")
		}
		return nil
	}
	if _, err := store.PutObject(ctx, "saved", "uncertain", bytes.NewBufferString("lost"), storage.PutOptions{RequestKey: "unknown-request"}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("interruption: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return saveStore(t, store.dataDir)
}

func TestRuntimeReopensAfterInterruptedPublishing(t *testing.T) {
	store := interruptedRuntimeStore(t)
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendFilesystem, MaxBytes: 5, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatalf("recovery must preserve observation access: %v", err)
	}
	defer instance.Close()
	assertRecoveryObservations(t, instance)
	assertRecoveryMutationRefusal(t, instance)
	if _, err := os.Stat(store.pendingPath()); err != nil {
		t.Fatal("pending evidence removed:", err)
	}
}

func assertRecoveryObservations(t *testing.T, instance *stowruntime.Instance) {
	t.Helper()
	ctx := context.Background()
	object, err := instance.GetObject(ctx, "saved", "known")
	if err != nil || string(object.Data) != "saved" || instance.Usage() != (stowruntime.Usage{Bytes: 5, Objects: 1}) {
		t.Fatalf("read=%+v err=%v usage=%+v", object, err, instance.Usage())
	}
	known, err := instance.ResolveSave(ctx, "saved", "known", "known-request")
	if err != nil || known.Outcome != stowruntime.SaveCommitted {
		t.Fatalf("known receipt=%+v err=%v", known, err)
	}
	unknown, err := instance.ResolveSave(ctx, "saved", "uncertain", "unknown-request")
	if !errors.Is(err, storage.ErrSaveRequestUnknown) || unknown.Outcome != stowruntime.SaveUnknown {
		t.Fatalf("unknown receipt=%+v err=%v", unknown, err)
	}
}

func assertRecoveryMutationRefusal(t *testing.T, instance *stowruntime.Instance) {
	t.Helper()
	ctx := context.Background()
	operations := []func() error{
		func() error { return instance.CreateBucket(ctx, "blocked") },
		func() error {
			_, err := instance.PutObject(ctx, "saved", "next", []byte("x"), stowruntime.PutOptions{})
			return err
		},
		func() error { return instance.DeleteObject(ctx, "saved", "known") },
		func() error {
			_, err := instance.CreateMultipartUpload(ctx, "saved", "part", storage.MultipartOptions{})
			return err
		},
	}
	for index, operation := range operations {
		if err := operation(); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Errorf("mutation %d bypassed recovery: %v", index, err)
		}
	}
}
