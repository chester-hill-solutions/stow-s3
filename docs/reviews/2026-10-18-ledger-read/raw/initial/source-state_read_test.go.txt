package capacity

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func ledgerReadHost(t testing.TB) *Host {
	t.Helper()
	host, err := Open(HostOptions{Dir: filepath.Join(t.TempDir(), "host"), MaxBytes: 128 << 20, MaxNamespaces: 1})
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func sparseLedger(t testing.TB, host *Host, size int64) os.FileInfo {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(host.dir, "capacity.json"), os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(host.dir, "capacity.json"))
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestLedgerReadAllocationBound(t *testing.T) {
	for _, size := range []int64{2*maxStateBytes + 1, 8*maxStateBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			host := ledgerReadHost(t)
			before := sparseLedger(t, host, size)
			runtime.GC()
			var start, end runtime.MemStats
			runtime.ReadMemStats(&start)
			_, err := Open(host.options)
			runtime.ReadMemStats(&end)
			if !errors.Is(err, ErrUnknown) {
				t.Fatalf("oversized ledger result=%v", err)
			}
			allocated := end.TotalAlloc - start.TotalAlloc
			t.Logf("ledger bytes=%d allocated bytes=%d", size, allocated)
			if allocated > maxStateBytes+(1<<20) {
				t.Errorf("oversized ledger was materialized: size=%d allocated=%d budget=%d", size, allocated, maxStateBytes+(1<<20))
			}
			after, err := os.Stat(filepath.Join(host.dir, "capacity.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("refused ledger was modified")
			}
		})
	}
}

func TestLedgerReadCanonicalSizeBoundary(t *testing.T) {
	host := ledgerReadHost(t)
	state := &hostState{Version: 1, MaxBytes: host.options.MaxBytes, MaxNamespaces: host.options.MaxNamespaces, Namespaces: map[string]*namespaceState{"project": {
		Options: NamespaceOptions{ID: "project", MaxBytes: 64 << 20, RecoveryReserveBytes: 32 << 20, MaxOperations: 256, MaxDependencies: 32},
		Stores:  map[string]*storeState{"store": {Root: "/"}}, Operations: map[string]operationState{},
	}}}
	base, err := encodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	// Root is an existing unbounded schema field. Padding it exercises the exact
	// encoded limit; this fixture does not claim filesystem support for that path.
	state.Namespaces["project"].Stores["store"].Root += strings.Repeat("r", maxStateBytes-len(base))
	encoded, err := encodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != maxStateBytes {
		t.Fatalf("boundary fixture bytes=%d", len(encoded))
	}
	if err := host.write(state); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(host.options)
	if err != nil {
		t.Fatalf("canonical ledger at size limit refused: %v", err)
	}
	restored, err := reopened.read()
	if err != nil || len(restored.Namespaces["project"].Stores["store"].Root) != len(state.Namespaces["project"].Stores["store"].Root) {
		t.Fatalf("boundary ledger changed: %v", err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join(host.dir, "capacity.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(host.options); !errors.Is(err, ErrUnknown) {
		t.Fatalf("ledger beyond size limit accepted: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, encoded) {
		t.Fatalf("over-limit ledger changed: %v", err)
	}
}

func TestLedgerReadMissingAndEmpty(t *testing.T) {
	host := ledgerReadHost(t)
	path := filepath.Join(host.dir, "capacity.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	fresh, err := Open(host.options)
	if err != nil {
		t.Fatalf("missing ledger did not initialize: %v", err)
	}
	state, err := fresh.read()
	if err != nil || state.Version != 1 || len(state.Namespaces) != 0 {
		t.Fatalf("fresh state=%+v err=%v", state, err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(host.options); !errors.Is(err, ErrUnknown) {
		t.Fatalf("empty ledger was reinitialized: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatalf("empty refused ledger changed: %v", err)
	}
}

func TestLedgerReadNativeErrors(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		host := ledgerReadHost(t)
		path := filepath.Join(host.dir, "capacity.json")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		_, err := Open(host.options)
		var native *os.PathError
		if !errors.As(err, &native) || native.Path != path || native.Op != "read" || !errors.Is(err, syscall.EISDIR) {
			t.Fatalf("directory read error changed: %v", err)
		}
	})
	t.Run("permission", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("requires enforced non-root file permissions")
		}
		host := ledgerReadHost(t)
		path := filepath.Join(host.dir, "capacity.json")
		if err := os.Chmod(path, 0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0600) })
		_, err := Open(host.options)
		var native *os.PathError
		if !errors.As(err, &native) || native.Path != path || native.Op != "open" || !errors.Is(err, os.ErrPermission) {
			t.Fatalf("permission error changed: %v", err)
		}
	})
}
