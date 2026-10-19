package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestSaveCapacityRefusesBeforeIntent(t *testing.T) {
	for _, kind := range []string{"ordinary", "guard", "request", "request-guard"} {
		for _, size := range []int{1600000, 4 << 20} {
			t.Run(fmt.Sprintf("%s/bytes=%d", kind, size), func(t *testing.T) { checkSaveCapacityRefusal(t, kind, size) })
		}
	}
}

func checkSaveCapacityRefusal(t *testing.T, kind string, size int) {
	t.Helper()
	namespace := publicationNamespace(t)
	store := publicationStore(t, namespace, t.TempDir())
	if err := store.reconcileCapacityLocked(); err != nil {
		t.Fatal(err)
	}
	before, err := namespace.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	opts := storage.PutOptions{}
	if strings.Contains(kind, "request") {
		opts.RequestKey = "refused"
	}
	if strings.Contains(kind, "guard") {
		opts.Guard = &storage.WriteGuard{Absent: true}
	}
	_, err = store.PutObject(context.Background(), "bucket", "key", strings.NewReader(strings.Repeat("x", size)), opts)
	if !errors.Is(err, capacity.ErrFull) {
		t.Errorf("over-budget %s write: %v", kind, err)
	}
	if _, err := store.HeadObject(context.Background(), "bucket", "key"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Errorf("refused object visible: %v", err)
	}
	if _, err := os.Stat(store.pendingPath()); !os.IsNotExist(err) {
		t.Errorf("refused save wrote intent: %v", err)
	}
	if len(store.saves.entries) != 0 {
		t.Errorf("refused save retained receipts: %d", len(store.saves.entries))
	}
	after, err := namespace.Snapshot(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Errorf("refusal changed charge: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestSaveCapacityChargesSuccessAndReplay(t *testing.T) {
	ctx := context.Background()
	namespace := publicationNamespace(t)
	store := publicationStore(t, namespace, t.TempDir())
	opts := storage.PutOptions{RequestKey: "saved", Guard: &storage.WriteGuard{Absent: true}}
	meta, err := store.PutObject(ctx, "bucket", "key", strings.NewReader("payload"), opts)
	if err != nil {
		t.Fatal(err)
	}
	assertPublicationCharge(t, store, namespace)
	before, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := store.PutObject(ctx, "bucket", "key", strings.NewReader("payload"), opts)
	if err != nil || !reflect.DeepEqual(meta, replayed) {
		t.Fatalf("replay=%+v, %v", replayed, err)
	}
	after, err := namespace.Snapshot(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("replay changed charge: before=%+v after=%+v err=%v", before, after, err)
	}
	if err := store.reconcileCapacityLocked(); err != nil {
		t.Fatal(err)
	}
	reconciled, err := namespace.Snapshot(ctx)
	if err != nil || !reflect.DeepEqual(after, reconciled) {
		t.Fatalf("successful save left stale ledger: before=%+v scan=%+v err=%v", after, reconciled, err)
	}
}

func TestSaveCapacitySharesNamespace(t *testing.T) {
	ctx := context.Background()
	namespace := publicationNamespace(t)
	first := publicationStore(t, namespace, t.TempDir())
	second := publicationStore(t, namespace, t.TempDir())
	body := strings.Repeat("x", 1<<20)
	if _, err := first.PutObject(ctx, "bucket", "key", strings.NewReader(body), storage.PutOptions{RequestKey: "first"}); err != nil {
		t.Fatal(err)
	}
	_, err := second.PutObject(ctx, "bucket", "key", strings.NewReader(body), storage.PutOptions{RequestKey: "second"})
	if !errors.Is(err, capacity.ErrFull) {
		t.Fatalf("shared quota ignored: %v", err)
	}
	if _, err := second.HeadObject(ctx, "bucket", "key"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("refused second object visible: %v", err)
	}
}

func TestSaveCapacityRetainsInterruptedReservation(t *testing.T) {
	for _, phase := range []string{"prepared", "publishing", "published", "sync", "terminal"} {
		t.Run(phase, func(t *testing.T) { checkSaveCapacityInterruption(t, phase) })
	}
}

func checkSaveCapacityInterruption(t *testing.T, phase string) {
	t.Helper()
	ctx := context.Background()
	namespace := publicationNamespace(t)
	first := publicationStore(t, namespace, t.TempDir())
	second := publicationStore(t, namespace, t.TempDir())
	injected := errors.New("interrupted save")
	first.saveFault = func(current string) error {
		if current == phase {
			return injected
		}
		return nil
	}
	body := strings.Repeat("x", 1000000)
	_, err := first.PutObject(ctx, "bucket", "key", strings.NewReader(body), storage.PutOptions{RequestKey: "interrupted"})
	if !errors.Is(err, injected) {
		t.Fatalf("interruption lost: %v", err)
	}
	snapshot, err := namespace.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if phase == "terminal" {
		assertPublicationCharge(t, first, namespace)
	} else if snapshot.PayloadBytes < int64(len(body)) || snapshot.PendingBytes < int64(len(body)) {
		t.Fatalf("interrupted save released reservation: %+v", snapshot)
	}
	if phase != "terminal" {
		if _, err := second.PutObject(ctx, "bucket", "key", strings.NewReader(body), storage.PutOptions{}); !errors.Is(err, capacity.ErrFull) {
			t.Fatalf("uncertain save allowed competing write: %v", err)
		}
	}
	first.saveFault = nil
	if !resolveCapacityInterruption(t, phase, first) {
		return
	}
	if _, err := second.PutObject(ctx, "bucket", "key", strings.NewReader(body), storage.PutOptions{}); err != nil {
		t.Fatalf("verified resolution did not permit next write: %v", err)
	}
}

func resolveCapacityInterruption(t *testing.T, phase string, store *FilesystemStore) bool {
	t.Helper()
	ctx := context.Background()
	receipt, err := store.ResolveSaveRequest(ctx, "bucket", "key", "interrupted")
	if phase == "publishing" {
		if !errors.Is(err, storage.ErrSaveRequestUnknown) || receipt.Outcome != "unknown" {
			t.Fatalf("unpublished publishing intent=%+v, %v", receipt, err)
		}
		return false
	}
	if phase == "prepared" {
		if !errors.Is(err, storage.ErrSaveRequestNotCommitted) || receipt.Outcome != "not_committed" {
			t.Fatalf("prepared intent=%+v, %v", receipt, err)
		}
	} else if err != nil || receipt.Outcome != "committed" {
		t.Fatalf("published intent=%+v, %v", receipt, err)
	}
	settled, err := store.namespace.Snapshot(ctx)
	if err != nil || settled.PendingBytes != 0 {
		t.Fatalf("resolution did not release staging: %+v, %v", settled, err)
	}
	if phase == "prepared" {
		if settled.PayloadBytes != 0 {
			t.Fatalf("absent object retained payload: %+v", settled)
		}
	} else {
		assertPublicationCharge(t, store, store.namespace)
	}
	return true
}

func TestSaveCapacityReservesReceiptSpace(t *testing.T) {
	ctx := context.Background()
	namespace := publicationNamespace(t)
	store := publicationStore(t, namespace, t.TempDir())
	for n := 0; n < 32; n++ {
		key := strings.Repeat("k", n+1)
		opts := storage.PutOptions{RequestKey: key, Metadata: map[string]string{"large": strings.Repeat("m", 40000)}}
		_, err := store.PutObject(ctx, "bucket", key, strings.NewReader("x"), opts)
		if errors.Is(err, capacity.ErrFull) {
			if n == 0 {
				t.Fatal("first small save unexpectedly refused")
			}
			if len(store.saves.entries) != n {
				t.Fatal("refused save retained a receipt")
			}
			if _, err := os.Stat(store.pendingPath()); !os.IsNotExist(err) {
				t.Fatalf("refusal left intent: %v", err)
			}
			if _, err := store.HeadObject(ctx, "bucket", key); !errors.Is(err, storage.ErrObjectNotFound) {
				t.Fatalf("refusal published object: %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("save receipts exceeded the namespace recovery reserve")
}

func TestSaveCapacityReopenSettlesReservation(t *testing.T) {
	for _, phase := range []string{"prepared", "published"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			namespace := publicationNamespace(t)
			dir := t.TempDir()
			store := publicationStore(t, namespace, dir)
			store.saveFault = func(current string) error {
				if current == phase {
					return errors.New("interrupted")
				}
				return nil
			}
			if _, err := store.PutObject(ctx, "bucket", "key", strings.NewReader("payload"), storage.PutOptions{RequestKey: "reopen"}); !errors.Is(err, storage.ErrSaveRequestUnknown) {
				t.Fatalf("save=%v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewFilesystemStoreWithNamespace(dir, namespace)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			receipt, err := reopened.ResolveSaveRequest(ctx, "bucket", "key", "reopen")
			if phase == "prepared" {
				if !errors.Is(err, storage.ErrSaveRequestNotCommitted) || receipt.Outcome != "not_committed" {
					t.Fatalf("receipt=%+v, %v", receipt, err)
				}
			} else if err != nil || receipt.Outcome != "committed" {
				t.Fatalf("receipt=%+v, %v", receipt, err)
			}
			snapshot, err := namespace.Snapshot(ctx)
			if err != nil || snapshot.PendingBytes != 0 {
				t.Fatalf("reopen reservation=%+v, %v", snapshot, err)
			}
			if phase == "published" {
				assertPublicationCharge(t, reopened, namespace)
			} else if snapshot.PayloadBytes != 0 {
				t.Fatalf("not-committed save charged payload: %+v", snapshot)
			}
		})
	}
}
