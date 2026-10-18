package fs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type capacityFenceFixture struct {
	store, other       *FilesystemStore
	namespace          *capacity.Namespace
	corridor, id, path string
}

func newCapacityFenceFixture(t *testing.T, kind string) *capacityFenceFixture {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("requires enforced non-root directory permissions")
	}
	ns := publicationNamespace(t)
	corridor := filepath.Join(t.TempDir(), "corridor")
	store := publicationStore(t, ns, filepath.Join(corridor, "store"))
	f := &capacityFenceFixture{store: store, other: publicationStore(t, ns, t.TempDir()), namespace: ns, corridor: corridor, path: store.objectPath("bucket", "key")}
	t.Cleanup(func() { _ = os.Chmod(corridor, 0755) })
	if strings.HasPrefix(kind, "part") {
		f.id = multipartForCapacity(t, store)
		f.path = filepath.Join(store.multipartDir(f.id), "part-00001")
	}
	if strings.HasSuffix(kind, "overwrite") {
		if err := f.write("original"); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *capacityFenceFixture) write(body string) error {
	if f.id != "" {
		_, err := f.store.UploadPart(context.Background(), f.id, 1, strings.NewReader(body))
		return err
	}
	_, err := f.store.PutObject(context.Background(), "bucket", "key", strings.NewReader(body), storage.PutOptions{})
	return err
}

func (f *capacityFenceFixture) read(t *testing.T) string {
	t.Helper()
	if f.id != "" {
		data, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		parts, err := f.store.ListParts(context.Background(), f.id)
		if err != nil || len(parts) != 1 || parts[0].ETag != storage.ETagForBytes(data) {
			t.Fatalf("part consumer=%+v, %v", parts, err)
		}
		return string(data)
	}
	body, meta, err := f.store.GetObject(context.Background(), "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	closeErr := body.Close()
	if err != nil || closeErr != nil || meta.Size != int64(len(data)) {
		t.Fatalf("object consumer=%+v, %v, %v", meta, err, closeErr)
	}
	return string(data)
}

func TestCapacityFenceFailureRetainsReservation(t *testing.T) {
	for _, kind := range []string{"object-new", "object-overwrite", "part-new", "part-overwrite"} {
		for _, recovery := range []string{"retry", "reopen"} {
			t.Run(kind+"/"+recovery, func(t *testing.T) { checkCapacityFenceFailure(t, kind, recovery) })
		}
	}
}

func checkCapacityFenceFailure(t *testing.T, kind, recovery string) {
	t.Helper()
	f := newCapacityFenceFixture(t, kind)
	body := strings.Repeat("x", 1500000)
	if err := os.Chmod(f.corridor, 0300); err != nil {
		t.Fatal(err)
	}
	err := f.write(body)
	if !errors.Is(err, os.ErrPermission) || errors.Is(err, storage.ErrMutationCommitted) {
		t.Fatalf("fence result=%v", err)
	}
	if !errors.Is(err, capacity.ErrUnknown) {
		t.Errorf("visible failed write did not require reconciliation: %v", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "open" || pathErr.Path != f.corridor {
		t.Fatalf("wrong native fence: %v", err)
	}
	if f.read(t) != body {
		t.Fatal("write failed before publication; wrong failure seam")
	}
	f.namespace = reopenFenceNamespace(t, f.namespace)
	assertFenceReservation(t, f)
	checkFenceCompetitor(t, f)
	finishFenceRecovery(t, f, recovery)
}

func reopenFenceNamespace(t *testing.T, ns *capacity.Namespace) *capacity.Namespace {
	t.Helper()
	dir, id := ns.Binding()
	host, err := capacity.Open(capacity.HostOptions{Dir: dir, MaxBytes: 5 << 20})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := host.Bind(capacity.NamespaceOptions{ID: id, MaxBytes: 4 << 20, RecoveryReserveBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return reopened
}

func assertFenceReservation(t *testing.T, f *capacityFenceFixture) {
	t.Helper()
	info, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.namespace.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("visible=%d charged=%d pending=%d", info.Size(), snapshot.PayloadBytes, snapshot.PendingBytes)
	if snapshot.PayloadBytes != info.Size() || snapshot.PendingBytes != info.Size() {
		t.Errorf("visible failed write lost durable reservation: %+v file=%d", snapshot, info.Size())
	}
}

func checkFenceCompetitor(t *testing.T, f *capacityFenceFixture) {
	t.Helper()
	ctx := context.Background()
	var accepted []string
	info, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	physical := info.Size()
	for _, key := range []string{"competing-first", "competing-second"} {
		_, err := f.other.PutObject(ctx, "bucket", key, strings.NewReader(strings.Repeat("y", 1000000)), storage.PutOptions{})
		if err == nil {
			accepted = append(accepted, key)
			info, statErr := os.Stat(f.other.objectPath("bucket", key))
			if statErr != nil {
				t.Fatal(statErr)
			}
			physical += info.Size()
			t.Errorf("unreconciled write allowed competitor %s", key)
		} else if !errors.Is(err, capacity.ErrFull) {
			t.Fatal(err)
		}
	}
	if physical > 3<<20 {
		t.Errorf("visible payload exceeded namespace allowance: bytes=%d allowance=%d", physical, 3<<20)
	}
	for _, key := range accepted {
		if err := f.other.DeleteObject(ctx, "bucket", key); err != nil {
			t.Fatal(err)
		}
	}
}

func finishFenceRecovery(t *testing.T, f *capacityFenceFixture, recovery string) {
	t.Helper()
	ctx := context.Background()
	if err := os.Chmod(f.corridor, 0755); err != nil {
		t.Fatal(err)
	}
	if recovery == "reopen" {
		dir := f.store.dataDir
		if err := f.store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := NewFilesystemStoreWithNamespace(dir, f.namespace)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reopened.Close() })
		f.store = reopened
	} else if err := f.write("settled"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.namespace.Snapshot(ctx)
	if err != nil || snapshot.PendingBytes != 0 || snapshot.PayloadBytes != info.Size() {
		t.Fatalf("verified recovery=%+v file=%d err=%v", snapshot, info.Size(), err)
	}
	if _, err := f.other.PutObject(ctx, "bucket", "after", strings.NewReader(strings.Repeat("z", 500000)), storage.PutOptions{}); err != nil {
		t.Fatalf("verified recovery did not release staging: %v", err)
	}
}

func TestCapacityWriteFailureBeforeRenameCanReconcile(t *testing.T) {
	for _, kind := range []string{"object-overwrite", "part-overwrite"} {
		t.Run(kind, func(t *testing.T) { checkCapacityPreRenameFailure(t, kind) })
	}
}

func checkCapacityPreRenameFailure(t *testing.T, kind string) {
	t.Helper()
	f := newCapacityFenceFixture(t, kind)
	dir := filepath.Dir(f.path)
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	err := f.write(strings.Repeat("x", 1000000))
	if !errors.Is(err, os.ErrPermission) || !errors.Is(err, capacity.ErrUnknown) || errors.Is(err, storage.ErrMutationCommitted) {
		t.Errorf("uncertain writer error=%v", err)
	}
	if f.read(t) != "original" {
		t.Fatal("pre-rename failure changed the visible object")
	}
	snapshot, err := f.namespace.Snapshot(context.Background())
	if err != nil || snapshot.PendingBytes < 1000000 {
		t.Errorf("failed writer reservation=%+v, %v", snapshot, err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := f.write("reconciled"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.namespace.Snapshot(context.Background())
	if err != nil || snapshot.PendingBytes != 0 {
		t.Fatalf("verified old-state reconciliation=%+v, %v", snapshot, err)
	}
	if f.read(t) != "reconciled" {
		t.Fatal("retry did not publish after verified reconciliation")
	}
}
