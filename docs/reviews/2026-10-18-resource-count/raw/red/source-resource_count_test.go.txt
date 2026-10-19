package capacity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func resourceCountNamespace(t testing.TB, counts [2]int) (*Namespace, [2]Usage) {
	t.Helper()
	dir := t.TempDir()
	host, err := Open(HostOptions{Dir: filepath.Join(dir, "host"), MaxBytes: 128 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ns, err := host.Bind(NamespaceOptions{ID: "count", MaxBytes: 64 << 20, RecoveryReserveBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	var usages [2]Usage
	for i, id := range []string{"left", "right"} {
		if err := ns.BindStore(id, filepath.Join(dir, id)); err != nil {
			t.Fatal(err)
		}
		resources := make([]Resource, counts[i])
		for j := range resources {
			resources[j] = Resource{StoreID: id, ID: fmt.Sprintf("%04d", j), Version: "v1", Bytes: 1, Class: Payload}
		}
		usages[i] = Usage{StoreID: id, Resources: resources}
		if err := ns.Reconcile(context.Background(), usages[i]); err != nil {
			t.Fatal(err)
		}
	}
	return ns, usages
}

func addCountResource(u Usage) Usage {
	resources := make([]Resource, len(u.Resources)+1)
	copy(resources, u.Resources)
	resources[len(u.Resources)] = Resource{StoreID: u.StoreID, ID: "extra", Version: "v1", Bytes: 1, Class: Payload}
	return Usage{StoreID: u.StoreID, Resources: resources}
}

func assertResourceCountSettled(t *testing.T, ns *Namespace, want int) {
	t.Helper()
	state, err := ns.host.read()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, store := range state.Namespaces[ns.id].Stores {
		count += len(store.Resources)
		if store.Pending != nil {
			t.Error("resource admission left a pending projection")
		}
	}
	if count != want {
		t.Errorf("settled resource count=%d, want=%d", count, want)
	}
	snapshot, err := ns.Snapshot(context.Background())
	if err != nil || snapshot.PayloadBytes != int64(want) {
		t.Errorf("resource snapshot=%+v, want payload=%d: %v", snapshot, want, err)
	}
}

func TestResourceCountGrowthRefusesBeforeEffect(t *testing.T) {
	ns, usages := resourceCountNamespace(t, [2]int{maxResources / 2, maxResources / 2})
	path := filepath.Join(ns.host.dir, "capacity.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "published")
	called := false
	err = ns.WithAdmission(context.Background(), Admission{Usage: addCountResource(usages[0])}, func() error { called = true; return os.WriteFile(marker, []byte("effect"), 0600) })
	if !errors.Is(err, ErrFull) || called {
		t.Errorf("resource-limit refusal ran effect: err=%v called=%t", err, called)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("resource-limit refusal published marker: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("resource-limit refusal changed durable ledger")
	}
	assertResourceCountSettled(t, ns, maxResources)
}

func TestResourceCountConcurrentAdmissions(t *testing.T) {
	ns, usages := resourceCountNamespace(t, [2]int{maxResources / 2, maxResources/2 - 1})
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	var effects atomic.Int32
	for _, u := range usages {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- ns.WithAdmission(context.Background(), Admission{Usage: addCountResource(u)}, func() error { effects.Add(1); return nil })
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	accepted, refused := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrFull) {
			refused++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || refused != 1 || effects.Load() != 1 {
		t.Errorf("last resource slot: accepted=%d refused=%d effects=%d", accepted, refused, effects.Load())
	}
	assertResourceCountSettled(t, ns, maxResources)
}

func reopenResourceCountNamespace(t *testing.T, ns *Namespace) *Namespace {
	t.Helper()
	host, err := Open(ns.host.options)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := host.Bind(NamespaceOptions{ID: "count", MaxBytes: 64 << 20, RecoveryReserveBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return reopened
}

func TestResourceCountUnknownGrowthRetainsSlot(t *testing.T) {
	ns, usages := resourceCountNamespace(t, [2]int{maxResources/2 - 1, maxResources / 2})
	ctx := context.Background()
	if err := ns.WithAdmission(ctx, Admission{Usage: addCountResource(usages[0])}, func() error { return ErrUnknown }); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	ns = reopenResourceCountNamespace(t, ns)
	called := false
	next := Admission{Usage: addCountResource(usages[1])}
	err := ns.WithAdmission(ctx, next, func() error { called = true; return nil })
	if !errors.Is(err, ErrFull) || called {
		t.Errorf("unknown growth allowed competing publication: err=%v called=%t", err, called)
	}
	if err := ns.Reconcile(ctx, usages[0]); err != nil {
		t.Fatal(err)
	}
	if err := ns.WithAdmission(ctx, next, func() error { return nil }); err != nil {
		t.Fatalf("verified old state did not release resource slot: %v", err)
	}
	assertResourceCountSettled(t, ns, maxResources)
}

func TestResourceCountUnknownDeletionRetainsSlot(t *testing.T) {
	ns, usages := resourceCountNamespace(t, [2]int{maxResources / 2, maxResources / 2})
	ctx := context.Background()
	removed := Usage{StoreID: usages[0].StoreID, Resources: usages[0].Resources[1:]}
	if err := ns.WithAdmission(ctx, Admission{Usage: removed}, func() error { return ErrUnknown }); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	ns = reopenResourceCountNamespace(t, ns)
	called := false
	next := Admission{Usage: addCountResource(usages[1])}
	err := ns.WithAdmission(ctx, next, func() error { called = true; return nil })
	if !errors.Is(err, ErrFull) || called {
		t.Errorf("unknown deletion freed resource slot before verification: err=%v called=%t", err, called)
	}
	if err := ns.Reconcile(ctx, removed); err != nil {
		t.Fatal(err)
	}
	if err := ns.WithAdmission(ctx, next, func() error { return nil }); err != nil {
		t.Fatalf("verified deletion did not release resource slot: %v", err)
	}
	assertResourceCountSettled(t, ns, maxResources)
}

func TestResourceCountReplacementAndDeletionAtLimit(t *testing.T) {
	for _, counts := range [][2]int{{maxResources, 0}, {maxResources / 2, maxResources / 2}} {
		t.Run(fmt.Sprint(counts), func(t *testing.T) {
			ns, usages := resourceCountNamespace(t, counts)
			ctx := context.Background()
			replacement := Usage{StoreID: usages[0].StoreID, Resources: append([]Resource(nil), usages[0].Resources...)}
			replacement.Resources[0].Version = "v2"
			if err := ns.WithAdmission(ctx, Admission{Usage: replacement}, func() error { return nil }); err != nil {
				t.Fatalf("same-count replacement refused: %v", err)
			}
			removed := Usage{StoreID: replacement.StoreID, Resources: replacement.Resources[1:]}
			if err := ns.WithAdmission(ctx, Admission{Usage: removed}, func() error { return nil }); err != nil {
				t.Fatalf("deletion at resource limit refused: %v", err)
			}
			if err := ns.WithAdmission(ctx, Admission{Usage: addCountResource(usages[1])}, func() error { return nil }); err != nil {
				t.Fatalf("released resource slot unavailable: %v", err)
			}
			assertResourceCountSettled(t, ns, maxResources)
		})
	}
}
