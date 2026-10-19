package capacity

import (
	"context"
	"errors"
	"testing"
)

func TestAdmissionRetainsUnknownEffectUntilReconciliation(t *testing.T) {
	ctx := context.Background()
	namespace := budget(t)
	first := usage(resource("a", 1000, Payload))
	if err := namespace.Reconcile(ctx, first); err != nil {
		t.Fatal(err)
	}
	admission := Admission{Usage: usage(resource("a", 2000, Payload)), StagingBytes: 2000, RecoveryBytes: 100}
	if err := namespace.WithAdmission(ctx, admission, func() error { return errors.Join(ErrUnknown, errors.New("publication uncertain")) }); !errors.Is(err, ErrUnknown) {
		t.Fatalf("effect=%v", err)
	}
	host, err := Open(namespace.host.options)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := host.Bind(NamespaceOptions{ID: "project", MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.Snapshot(ctx)
	if err != nil || snapshot.PayloadBytes != 2000 || snapshot.PendingBytes != 2000 {
		t.Fatalf("unknown reservation lost across reopen: %+v, %v", snapshot, err)
	}
	called := false
	if err := reopened.WithAdmission(ctx, admission, func() error { called = true; return nil }); !errors.Is(err, ErrUnknown) || called {
		t.Fatalf("unreconciled admission ran: %v, called=%v", err, called)
	}
	if err := reopened.Reconcile(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := reopened.WithAdmission(ctx, admission, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	settled, err := reopened.Snapshot(ctx)
	if err != nil || settled.PayloadBytes != 2000 || settled.PendingBytes != 0 {
		t.Fatalf("reconciled admission=%+v, %v", settled, err)
	}
}
