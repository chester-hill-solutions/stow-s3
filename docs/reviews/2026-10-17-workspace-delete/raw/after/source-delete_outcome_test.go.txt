package workspace_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestWorkspaceDeleteOutcome(t *testing.T) {
	for _, bound := range []bool{false, true} {
		for _, batch := range []bool{false, true} {
			for _, fault := range []bool{false, true} {
				name := fmt.Sprintf("runtime=%t/batch=%t/fault=%t", bound, batch, fault)
				t.Run(name, func(t *testing.T) { checkWorkspaceDeleteOutcome(t, bound, batch, fault) })
			}
		}
	}
}

func checkWorkspaceDeleteOutcome(t *testing.T, bound, batch, fault bool) {
	t.Helper()
	store, root := newStore(t)
	put(t, store, "victim", "aaaa")
	put(t, store, "survivor", "bbb")
	deleteOne, deleteMany := store.DeleteObject, store.DeleteObjects
	var instance *stowruntime.Instance
	if bound {
		var err error
		instance, err = stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendWorkspace, MaxBytes: 8, MaxObjects: 2}, store, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		deleteOne, deleteMany = instance.DeleteObject, instance.DeleteObjects
	}
	if fault {
		restore := blockPartUploadDocument(t, workspace.InternalPath(root, "index.json"))
		defer restore()
	}
	ctx := context.Background()
	var deleted []string
	var err error
	if batch {
		deleted, err = deleteMany(ctx, bucket, []string{"victim", "survivor"})
	} else {
		err = deleteOne(ctx, bucket, "victim")
	}
	assertWorkspaceDeleteError(t, fault, err)
	if _, err := store.HeadObject(ctx, bucket, "victim"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Errorf("confirmed victim still present: %v", err)
	}
	want := []string{"victim"}
	if batch && !fault {
		want = append(want, "survivor")
	}
	if batch && !slices.Equal(deleted, want) {
		t.Errorf("confirmed keys=%v want=%v", deleted, want)
	}
	assertDeleteSurvivor(t, store, batch && !fault)
	assertDeleteUsage(t, instance, batch && !fault)
}

func assertWorkspaceDeleteError(t *testing.T, fault bool, err error) {
	t.Helper()
	if !fault {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if !errors.Is(err, storage.ErrMutationCommitted) {
		t.Errorf("deleted file lost committed outcome: %v", err)
	}
	var pathErr *os.LinkError
	if !errors.As(err, &pathErr) {
		t.Errorf("original index filesystem error lost: %v", err)
	}
}

func assertDeleteSurvivor(t *testing.T, store *workspace.Store, removed bool) {
	t.Helper()
	if removed {
		if _, err := store.HeadObject(context.Background(), bucket, "survivor"); !errors.Is(err, storage.ErrObjectNotFound) {
			t.Errorf("healthy batch left survivor: %v", err)
		}
		return
	}
	if got := get(t, store, "survivor"); got != "bbb" {
		t.Errorf("unprocessed key changed: %q", got)
	}
}

func assertDeleteUsage(t *testing.T, instance *stowruntime.Instance, allRemoved bool) {
	t.Helper()
	if instance == nil {
		return
	}
	want := stowruntime.Usage{Bytes: 3, Objects: 1}
	if allRemoved {
		want = stowruntime.Usage{}
	}
	if got := instance.Usage(); got != want {
		t.Errorf("immediate accounting=%+v want=%+v", got, want)
	}
}

func TestWorkspaceDeleteRefusalHasNoCommittedOutcome(t *testing.T) {
	store, _ := newStore(t)
	put(t, store, "victim", "aaaa")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := store.DeleteObject(ctx, bucket, "victim")
	if !errors.Is(err, context.Canceled) || errors.Is(err, storage.ErrMutationCommitted) {
		t.Errorf("ordinary refusal changed: %v", err)
	}
	if got := get(t, store, "victim"); got != "aaaa" {
		t.Errorf("refused deletion changed bytes: %q", got)
	}
}
