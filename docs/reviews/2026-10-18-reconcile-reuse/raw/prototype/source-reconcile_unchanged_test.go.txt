package capacity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func unchangedReconcileFixture(t *testing.T) (*Namespace, Usage) {
	t.Helper()
	ns := budget(t)
	u := usage(resource("a", 1000, Payload), resource("b", 2000, Payload))
	if err := ns.Reconcile(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return ns, u
}

func holdReconcileLedger(t *testing.T, ns *Namespace) (*os.File, os.FileInfo, []byte) {
	t.Helper()
	f, err := os.Open(filepath.Join(ns.host.dir, "capacity.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return f, info, data
}

func TestReconcileUnchangedKeepsLedger(t *testing.T) {
	ns, u := unchangedReconcileFixture(t)
	f, before, data := holdReconcileLedger(t, ns)
	u.Resources = slices.Clone(u.Resources)
	if err := ns.Reconcile(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Error("unchanged reconciliation replaced the ledger")
	}
	got, err := os.ReadFile(f.Name())
	if err != nil || !bytes.Equal(data, got) {
		t.Fatalf("unchanged bytes changed: %v", err)
	}
	snapshot, err := ns.Snapshot(context.Background())
	if err != nil || snapshot.PayloadBytes != 3000 || snapshot.PendingBytes != 0 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestReconcileUnchangedChangesReplaceLedger(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Usage)
	}{
		{"version", func(u *Usage) { u.Resources[0].Version = "v2" }},
		{"bytes", func(u *Usage) { u.Resources[0].Bytes++ }},
		{"id", func(u *Usage) { u.Resources[0].ID = "new" }},
		{"class", func(u *Usage) { u.Resources[0].Class = Recovery }},
		{"store-field", func(u *Usage) { u.Resources[0].StoreID = "new" }},
		{"order", func(u *Usage) { u.Resources[0], u.Resources[1] = u.Resources[1], u.Resources[0] }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ns, u := unchangedReconcileFixture(t)
			f, before, _ := holdReconcileLedger(t, ns)
			c.change(&u)
			if err := ns.Reconcile(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(f.Name())
			if err != nil || os.SameFile(before, after) {
				t.Fatalf("changed usage did not replace ledger: %v", err)
			}
			state, err := ns.host.read()
			if err != nil || !slices.Equal(state.Namespaces[ns.id].Stores[u.StoreID].Resources, u.Resources) {
				t.Fatalf("changed usage not persisted: %v", err)
			}
		})
	}
	t.Run("nil-to-empty", func(t *testing.T) {
		ns := budget(t)
		u := usage()
		f, before, _ := holdReconcileLedger(t, ns)
		u.Resources = []Resource{}
		if err := ns.Reconcile(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(f.Name())
		if err != nil || os.SameFile(before, after) {
			t.Fatalf("nil-to-empty was not persisted: %v", err)
		}
		state, err := ns.host.read()
		if err != nil || state.Namespaces[ns.id].Stores[u.StoreID].Resources == nil {
			t.Fatalf("empty resources not persisted: %v", err)
		}
	})
}

func TestReconcileUnchangedReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires enforced non-root permissions")
	}
	ns, u := unchangedReconcileFixture(t)
	t.Cleanup(func() { _ = os.Chmod(ns.host.dir, 0755) })
	if err := os.Chmod(ns.host.dir, 0555); err != nil {
		t.Fatal(err)
	}
	if err := ns.Reconcile(context.Background(), u); err != nil {
		t.Fatalf("unchanged reconciliation required replacement: %v", err)
	}
	u.Resources[0].Bytes++
	if err := ns.Reconcile(context.Background(), u); !errors.Is(err, ErrUnknown) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("changed read-only reconciliation=%v", err)
	}
}

func TestReconcileUnchangedFallback(t *testing.T) {
	for _, writable := range []bool{true, false} {
		name := "repair"
		if !writable {
			name = "refuse"
		}
		t.Run(name, func(t *testing.T) {
			if os.Geteuid() == 0 {
				t.Skip("requires enforced non-root permissions")
			}
			ns, u := unchangedReconcileFixture(t)
			f, before, _ := holdReconcileLedger(t, ns)
			t.Cleanup(func() { _ = os.Chmod(ns.host.dir, 0755); _ = os.Chmod(f.Name(), 0600) })
			if err := os.Chmod(f.Name(), 0400); err != nil {
				t.Fatal(err)
			}
			if !writable {
				if err := os.Chmod(ns.host.dir, 0555); err != nil {
					t.Fatal(err)
				}
			}
			err := ns.Reconcile(context.Background(), u)
			after, statErr := os.Stat(f.Name())
			if statErr != nil {
				t.Fatal(statErr)
			}
			if writable {
				if err != nil || os.SameFile(before, after) || after.Mode().Perm() != 0600 {
					t.Fatalf("fallback failed: %v mode=%v", err, after.Mode())
				}
			} else if !errors.Is(err, ErrUnknown) || !errors.Is(err, os.ErrPermission) || !os.SameFile(before, after) {
				t.Fatalf("fallback refusal=%v", err)
			}
		})
	}
}

func TestReconcileUnchangedPendingStillWrites(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires enforced non-root permissions")
	}
	ns, u := unchangedReconcileFixture(t)
	ctx := context.Background()
	if err := ns.WithAdmission(ctx, Admission{Usage: u, StagingBytes: 100}, func() error { return ErrUnknown }); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	f, before, _ := holdReconcileLedger(t, ns)
	t.Cleanup(func() { _ = os.Chmod(ns.host.dir, 0755) })
	if err := os.Chmod(ns.host.dir, 0555); err != nil {
		t.Fatal(err)
	}
	if err := ns.Reconcile(ctx, u); !errors.Is(err, ErrUnknown) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("pending reconciliation skipped write: %v", err)
	}
	snapshot, err := ns.Snapshot(ctx)
	if err != nil || snapshot.PendingBytes != 100 {
		t.Fatalf("failed reconciliation lost pending: %+v %v", snapshot, err)
	}
	if err := os.Chmod(ns.host.dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ns.Reconcile(ctx, u); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(f.Name())
	if err != nil || os.SameFile(before, after) {
		t.Fatalf("pending settlement was not written: %v", err)
	}
	snapshot, err = ns.Snapshot(ctx)
	if err != nil || snapshot.PendingBytes != 0 {
		t.Fatalf("pending not cleared: %+v %v", snapshot, err)
	}
}

func TestReconcileUnchangedAncestorError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires enforced non-root permissions")
	}
	ns, u := unchangedReconcileFixture(t)
	parent := filepath.Dir(ns.host.dir)
	t.Cleanup(func() { _ = os.Chmod(parent, 0755) })
	if err := os.Chmod(parent, 0300); err != nil {
		t.Fatal(err)
	}
	err := ns.Reconcile(context.Background(), u)
	var native *os.PathError
	if !errors.Is(err, os.ErrPermission) || !errors.As(err, &native) || native.Op != "open" || native.Path != parent {
		t.Fatalf("ancestor fence was skipped: %v", err)
	}
	if err := os.Chmod(parent, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ns.Reconcile(context.Background(), u); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileUnchangedChecksQuota(t *testing.T) {
	ns, u := unchangedReconcileFixture(t)
	u.Resources[0].Bytes = 4 << 20
	state, err := ns.host.read()
	if err != nil {
		t.Fatal(err)
	}
	state.Namespaces[ns.id].Stores[u.StoreID].Resources = u.Resources
	encoded, err := encodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ns.host.dir, "capacity.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	f, before, _ := holdReconcileLedger(t, ns)
	if err := ns.Reconcile(context.Background(), u); !errors.Is(err, ErrFull) {
		t.Fatalf("unchanged over-quota state accepted: %v", err)
	}
	after, err := os.Stat(f.Name())
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("quota refusal changed ledger: %v", err)
	}
}
