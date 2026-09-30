package stow_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func recoveryNamespace(t *testing.T) *stow.CapacityNamespace {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("owned native filesystem profile")
	}
	host, err := stow.OpenCapacityHost(stow.CapacityHostOptions{Dir: filepath.Join(t.TempDir(), "host"), MaxBytes: 128 << 20})
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := host.BindNamespace(stow.CapacityNamespaceOptions{ID: "project", MaxBytes: 64 << 20, RecoveryReserveBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return namespace
}

func recoveryStore(t *testing.T, options stow.FilesystemOptions) *stow.Runtime {
	t.Helper()
	store, err := stow.OpenFilesystem(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func holdObservedObject(t *testing.T, store *stow.Runtime, id string) stow.ObjectRecoveryHoldOptions {
	t.Helper()
	_, condition, err := store.ReadForSave(context.Background(), "reports", "result")
	if err != nil {
		t.Fatal(err)
	}
	options := stow.ObjectRecoveryHoldOptions{ID: id, Owner: "consumer", Objects: []stow.ObjectRecoveryReference{{Bucket: "reports", Key: "result", Condition: condition}}}
	if err := store.BeginObjectRecoveryHold(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	return options
}

func TestObjectRecoveryHoldsPreserveObservedBytesAfterReopen(t *testing.T) {
	ctx := context.Background()
	namespace := recoveryNamespace(t)
	options := stow.FilesystemOptions{Dir: t.TempDir(), Namespace: namespace}
	store := recoveryStore(t, options)
	if !store.Capabilities().RecoveryHolds {
		t.Fatal("bound store omitted recovery capability")
	}
	if err := store.CreateBucket(ctx, "reports"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "reports", "result", []byte("original"), stow.PutOptions{Metadata: map[string]string{"stage": "ready"}}); err != nil {
		t.Fatal(err)
	}
	hold := holdObservedObject(t, store, "delivery")
	if err := store.BeginObjectRecoveryHold(ctx, hold); err != nil {
		t.Fatalf("same hold replay: %v", err)
	}
	holdObservedObject(t, store, "second-consumer")
	assertHeldMutationsRefused(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = recoveryStore(t, options)
	assertHeldAfterReopen(t, store)
	if err := store.ReleaseObjectRecoveryHold(ctx, "delivery", "consumer"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseObjectRecoveryHold(ctx, "delivery", "consumer"); err != nil {
		t.Fatalf("release replay: %v", err)
	}
	assertHeldAfterReopen(t, store)
	if err := store.ReleaseObjectRecoveryHold(ctx, "second-consumer", "consumer"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "reports", "result", []byte("replacement"), stow.PutOptions{}); err != nil {
		t.Fatalf("released overwrite: %v", err)
	}
}

// A pinned object refuses overwrite.
func assertHeldMutationsRefused(t *testing.T, store *stow.Runtime) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.PutObject(ctx, "reports", "result", []byte("replacement"), stow.PutOptions{}); !errors.Is(err, stow.ErrRecoveryHeld) {
		t.Fatalf("held overwrite = %v", err)
	}
	result, err := store.SaveObject(ctx, "reports", "result", []byte("replacement"), stow.SaveOptions{Condition: stow.ReplacementCondition()})
	if !errors.Is(err, stow.ErrRecoveryHeld) || result.Outcome != stow.SaveNotCommitted {
		t.Fatalf("held guarded save = %s, %v", result.Outcome, err)
	}
}

// Against a reopened store: a hold outlives the process, only its owner may
// release it, and the bytes are the ones that were read.
func assertHeldAfterReopen(t *testing.T, store *stow.Runtime) {
	t.Helper()
	ctx := context.Background()
	if err := store.DeleteObject(ctx, "reports", "result"); !errors.Is(err, stow.ErrRecoveryHeld) {
		t.Fatalf("held reopened deletion = %v", err)
	}
	if err := store.ReleaseObjectRecoveryHold(ctx, "delivery", "wrong-owner"); !errors.Is(err, stow.ErrCapacityConflict) {
		t.Fatalf("wrong owner release = %v", err)
	}
	object, err := store.GetObject(ctx, "reports", "result")
	if err != nil || string(object.Data) != "original" || object.Metadata["stage"] != "ready" {
		t.Fatalf("retained object = %#v, %v", object, err)
	}
}

func TestObjectRecoveryHoldConditionsAndAuthority(t *testing.T) {
	ctx := context.Background()
	store, err := stow.Open(stow.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.SupportsRecoveryHolds() {
		t.Fatal("memory advertised durable holds")
	}
	if err := store.ReleaseObjectRecoveryHold(ctx, "operation", "consumer"); !errors.Is(err, stow.ErrRecoveryHoldsUnsupported) {
		t.Fatalf("unsupported release = %v", err)
	}
	readOnly := stow.ReadOnly()
	denied := recoveryStore(t, stow.FilesystemOptions{Dir: t.TempDir(), Namespace: recoveryNamespace(t), Options: stow.Options{Authority: &readOnly}})
	if err := denied.BeginObjectRecoveryHold(ctx, stow.ObjectRecoveryHoldOptions{ID: "operation", Owner: "consumer", Objects: []stow.ObjectRecoveryReference{{Bucket: "reports", Key: "result", Condition: stow.ReplacementCondition()}}}); err == nil {
		t.Fatal("read-only hold admitted")
	} else {
		var auth *stow.ErrNotAuthorized
		if !errors.As(err, &auth) {
			t.Fatalf("hold denial = %v", err)
		}
	}
	if err := denied.ReleaseObjectRecoveryHold(ctx, "operation", "consumer"); err == nil {
		t.Fatal("read-only release admitted")
	}
}
