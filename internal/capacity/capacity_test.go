package capacity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// budget is a namespace with work room and a recovery reserve.
func budget(t *testing.T) *Namespace {
	t.Helper()
	host, err := Open(HostOptions{Dir: filepath.Join(t.TempDir(), "host"), MaxBytes: 8 << 20, MaxNamespaces: 4})
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := host.Bind(NamespaceOptions{ID: "project", MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := namespace.BindStore("store-1", filepath.Join(t.TempDir(), "store")); err != nil {
		t.Fatal(err)
	}
	return namespace
}

func resource(id string, bytes int64, class string) Resource {
	return Resource{StoreID: "store-1", ID: id, Version: id + "-v1", Bytes: bytes, Class: class}
}

func usage(resources ...Resource) Usage {
	return Usage{StoreID: "store-1", Resources: resources}
}

func TestReconcileAccountsPayloadAndRecoverySeparately(t *testing.T) {
	ctx := context.Background()
	namespace := budget(t)
	if err := namespace.Reconcile(ctx, usage(resource("a", 1000, Payload), resource("b", 200, Recovery))); err != nil {
		t.Fatal(err)
	}
	snapshot, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PayloadBytes != 1000 || snapshot.RecoveryBytes <= 200 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if err := namespace.Reconcile(ctx, usage(resource("a", 3<<20, Payload))); err != nil {
		t.Fatalf("payload at the reserved boundary: %v", err)
	}
	if err := namespace.Reconcile(ctx, usage(resource("a", 4<<20, Payload))); !errors.Is(err, ErrFull) {
		t.Fatalf("payload past the reserve = %v", err)
	}
	if err := namespace.Reconcile(ctx, usage(resource("a", 3<<20, Payload), resource("b", 2<<20, Recovery))); !errors.Is(err, ErrFull) {
		t.Fatalf("recovery past its reserve = %v", err)
	}
}

func TestReconcileRefusesUnboundStoreAndDuplicateResources(t *testing.T) {
	ctx := context.Background()
	namespace := budget(t)
	if err := namespace.Reconcile(ctx, Usage{StoreID: "other", Resources: []Resource{resource("a", 1, Payload)}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("unbound store = %v", err)
	}
	if err := namespace.Reconcile(ctx, usage(resource("a", 1, Payload), resource("a", 2, Payload))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate resource = %v", err)
	}
	if err := namespace.Reconcile(ctx, usage(Resource{StoreID: "store-1", ID: "a", Version: "v", Bytes: -1, Class: Payload})); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative bytes = %v", err)
	}
}

func TestBindStoreRefusesAStoreClaimedElsewhere(t *testing.T) {
	namespace := budget(t)
	if err := namespace.BindStore("store-2", "/tmp/elsewhere"); err != nil {
		t.Fatal(err)
	}
	if err := namespace.BindStore("store-1", "/tmp/elsewhere"); !errors.Is(err, ErrConflict) {
		t.Fatalf("one root under two stores = %v", err)
	}
	if err := namespace.BindStore("store-1", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty root = %v", err)
	}
}

func TestAdmissionPublishesOnlyAfterTheEffectSucceeds(t *testing.T) {
	ctx := context.Background()
	namespace := budget(t)
	first := usage(resource("a", 1000, Payload))
	if err := namespace.WithAdmission(ctx, Admission{Usage: first, StagingBytes: 1000}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	// A failed effect must leave nothing staged behind.
	failed := usage(resource("a", 2000, Payload))
	wantErr := errors.New("effect failed")
	if err := namespace.WithAdmission(ctx, Admission{Usage: failed, StagingBytes: 1000}, func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("failed effect = %v", err)
	}
	snapshot, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PayloadBytes != 1000 || snapshot.PendingBytes != 0 {
		t.Fatalf("failed effect left state behind: %+v", snapshot)
	}
}

func TestAdmissionRefusesAnUnusableRequest(t *testing.T) {
	ctx := context.Background()
	namespace := budget(t)
	admission := Admission{Usage: usage(resource("a", 1000, Payload)), StagingBytes: 1000}
	if err := namespace.WithAdmission(ctx, admission, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no effect = %v", err)
	}
	if err := namespace.WithAdmission(ctx, Admission{Usage: admission.Usage, StagingBytes: -1}, func() error { return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative staging = %v", err)
	}
	if err := namespace.WithAdmission(ctx, Admission{Usage: usage(Resource{StoreID: "store-1", ID: "a", Version: "v", Bytes: 1, Class: "unknown"}), StagingBytes: 1}, func() error { return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unclassed resource = %v", err)
	}
	// A refusal leaves no reservation behind; the next admission proves it.
	if err := namespace.WithAdmission(ctx, admission, func() error { return nil }); err != nil {
		t.Fatalf("admission after a refusal: %v", err)
	}
}

// A held dependency admits its own version and refuses any other.
func TestHeldDependencyRefusesReplacement(t *testing.T) {
	ctx := context.Background()
	namespace := budget(t)
	held := resource("a", 1000, Payload)
	if err := namespace.Reconcile(ctx, usage(held)); err != nil {
		t.Fatal(err)
	}
	operation := Operation{ID: "op-1", Owner: "consumer", Meaning: "delivery", Dependencies: []Resource{held}}
	if err := namespace.BeginOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
	if err := namespace.Reconcile(ctx, usage(held)); err != nil {
		t.Fatalf("unchanged held resource = %v", err)
	}
	replacement := held
	replacement.Version = "a-v2"
	if err := namespace.Reconcile(ctx, usage(replacement)); !errors.Is(err, ErrHeld) {
		t.Fatalf("replaced held resource = %v", err)
	}
	if err := namespace.CheckDependencies(ctx, []Resource{held}); !errors.Is(err, ErrHeld) {
		t.Fatalf("dependency check = %v", err)
	}
	if err := namespace.ReleaseOperation(ctx, "op-1", "consumer"); err != nil {
		t.Fatal(err)
	}
	if err := namespace.CheckDependencies(ctx, []Resource{held}); err != nil {
		t.Fatalf("released dependency check = %v", err)
	}
	if err := namespace.ReleaseOperation(ctx, "op-1", "consumer"); err != nil {
		t.Fatalf("release replay: %v", err)
	}
	if err := namespace.ReleaseOperation(ctx, "op-1", "someone-else"); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign release = %v", err)
	}
}

func TestBeginOperationRefusesAnUnverifiableDependency(t *testing.T) {
	ctx := context.Background()
	namespace := budget(t)
	operation := Operation{ID: "op-1", Owner: "consumer", Meaning: "delivery", Dependencies: []Resource{resource("a", 1000, Payload)}}
	if err := namespace.BeginOperation(ctx, operation); !errors.Is(err, ErrConflict) {
		t.Fatalf("dependency the store never reported = %v", err)
	}
	if err := namespace.BeginOperation(ctx, Operation{ID: "op-1", Owner: "consumer", Meaning: "delivery", Dependencies: []Resource{resource("a", -1, Payload)}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed dependency = %v", err)
	}
}

// Filling the payload budget must leave room to record its own release.
func TestOperationReserveIsChargedBeforePublication(t *testing.T) {
	ctx := context.Background()
	namespace := budget(t)
	if err := namespace.Reconcile(ctx, usage(resource("a", 3<<20, Payload))); err != nil {
		t.Fatal(err)
	}
	operation := Operation{ID: "op-1", Owner: "consumer", Meaning: "delivery", ReserveBytes: 1 << 20}
	if err := namespace.BeginOperation(ctx, operation); !errors.Is(err, ErrFull) {
		t.Fatalf("reserve past the recovery budget = %v", err)
	}
}

func TestHostStateSurvivesReopenAndRefusesTampering(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "host")
	options := HostOptions{Dir: dir, MaxBytes: 8 << 20, MaxNamespaces: 4}
	host, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := host.Bind(NamespaceOptions{ID: "project", MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := namespace.BindStore("store-1", filepath.Join(t.TempDir(), "store")); err != nil {
		t.Fatal(err)
	}
	if err := namespace.Reconcile(ctx, usage(resource("a", 1000, Payload))); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.Bind(NamespaceOptions{ID: "project", MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := again.BindStore("store-1", filepath.Join(t.TempDir(), "elsewhere")); !errors.Is(err, ErrConflict) {
		t.Fatalf("rebound store root = %v", err)
	}

	// A host reopened with a different budget is refused, not reconciled.
	widened := options
	widened.MaxBytes = 16 << 20
	if _, err := Open(widened); !errors.Is(err, ErrConflict) {
		t.Fatalf("widened host = %v", err)
	}

	path := filepath.Join(dir, "capacity.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), `"store-1"`, `"store-2"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(options); !errors.Is(err, ErrUnknown) {
		t.Fatalf("tampered host state = %v", err)
	}
}

func TestBindRefusesAnUnequalRebindAndHonoursItsBound(t *testing.T) {
	ctx := context.Background()
	host, err := Open(HostOptions{Dir: filepath.Join(t.TempDir(), "host"), MaxBytes: 8 << 20, MaxNamespaces: 1})
	if err != nil {
		t.Fatal(err)
	}
	options := NamespaceOptions{ID: "project", MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20}
	if _, err := host.Bind(options); err != nil {
		t.Fatal(err)
	}
	wider := options
	wider.MaxBytes = 5 << 20
	if _, err := host.Bind(wider); !errors.Is(err, ErrConflict) {
		t.Fatalf("silent rebind = %v", err)
	}
	if _, err := host.Bind(NamespaceOptions{ID: "other", MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20}); !errors.Is(err, ErrFull) {
		t.Fatalf("namespace past its bound = %v", err)
	}
	if _, err := host.Bind(wider); !errors.Is(err, ErrConflict) {
		t.Fatalf("unbound rebind = %v", err)
	}
	// The equal rebind is idempotent and consumes no slot.
	if _, err := host.Bind(options); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Bind(NamespaceOptions{ID: "third", MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20}); !errors.Is(err, ErrFull) {
		t.Fatalf("slot consumed by an equal rebind = %v", err)
	}
	if _, err := host.Bind(NamespaceOptions{ID: "reserved", MaxBytes: 4 << 20, RecoveryReserveBytes: 4 << 20}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reserve as large as the budget = %v", err)
	}
	_ = ctx
}
