package runthrough_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestPreparedRecoveryUncertainty(t *testing.T) {
	for _, backend := range []string{"memory", "filesystem"} {
		for _, operation := range []string{"put", "copy", "multipart", "delete"} {
			for _, entrypoint := range []string{"recover", "retry"} {
				for _, fault := range []string{"marked-missing", "wrapped-marked", "unknown"} {
					item := recoveryCase{backend, operation, entrypoint, fault}
					t.Run(fmt.Sprintf("%s/%s/%s/%s", backend, operation, entrypoint, fault), func(t *testing.T) { checkRecoveryUncertainty(t, item) })
				}
			}
		}
	}
}
func checkRecoveryUncertainty(t *testing.T, item recoveryCase) {
	t.Helper()
	f := newRecoveryFixture(t, item)
	f.local.failure = recoveryFailure(item.fault)
	if err := f.recover(item.entrypoint); err != f.local.failure {
		t.Errorf("original HEAD error lost: got=%v want=%v", err, f.local.failure)
	}
	if f.local.heads != 1 {
		t.Errorf("uncertain recovery read HEAD %d times", f.local.heads)
	}
	assertRecoveryHeld(t, f)
	journal, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(journal, f.journal) {
		t.Error("uncertain recovery rewrote the durable intent")
	}
	f.reopen(t)
	assertRecoveryHeld(t, f)
	f.local.failure = nil
	local, err := f.local.Store.HeadObject(context.Background(), "probe", "target")
	if err != nil || local.VersionID != f.version {
		t.Fatalf("local version changed: meta=%+v error=%v", local, err)
	}
	finishRecovery(t, f)
}
func assertRecoveryHeld(t *testing.T, f *recoveryFixture) {
	t.Helper()
	prepared, pending := f.outbox.Prepared(), f.outbox.Pending()
	if len(prepared) != 1 || len(pending) != 0 {
		t.Errorf("uncertain recovery changed intent: prepared=%+v pending=%+v", prepared, pending)
	}
	if len(prepared) == 1 && (prepared[0].ID != f.entry.ID || prepared[0].PreviousVersion != f.entry.PreviousVersion || prepared[0].UpstreamVersion != f.entry.UpstreamVersion) {
		t.Error("uncertain recovery changed version or upstream condition")
	}
	up := f.upstream
	if up.headCalls != 0 || up.getCalls != 0 || up.putCalls != 0 || up.delCalls != 0 {
		t.Errorf("uncertain recovery reached upstream: head=%d get=%d put=%d delete=%d", up.headCalls, up.getCalls, up.putCalls, up.delCalls)
	}
}
func finishRecovery(t *testing.T, f *recoveryFixture) {
	t.Helper()
	ctx := context.Background()
	if err := f.adapter.RetryPending(ctx); err != nil {
		t.Fatalf("healthy retry: %v", err)
	}
	if len(f.outbox.Prepared()) != 0 || len(f.outbox.Pending()) != 0 {
		t.Error("healthy recovery left an intent")
	}
	wantBody, wantPuts := "new", 1
	if f.entry.Operation == runthrough.OutboxDelete {
		wantBody, wantPuts = "old", 0
	}
	if f.upstream.putCalls != wantPuts || f.upstream.delCalls != 0 {
		t.Errorf("wrong replay: puts=%d deletes=%d", f.upstream.putCalls, f.upstream.delCalls)
	}
	if got := string(f.upstream.bodies[objectKey("probe", "target")]); got != wantBody {
		t.Errorf("upstream body=%q want=%q", got, wantBody)
	}
	if err := f.adapter.RetryPending(ctx); err != nil {
		t.Fatal(err)
	}
	if f.upstream.putCalls != wantPuts || f.upstream.delCalls != 0 {
		t.Error("second retry repeated a mutation")
	}
}
func TestPreparedRecoveryConfirmedMissing(t *testing.T) {
	for _, backend := range []string{"memory", "filesystem"} {
		for _, operation := range []string{"put", "copy", "multipart", "delete"} {
			for _, wrapped := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/wrapped=%t", backend, operation, wrapped), func(t *testing.T) {
					checkRecoveryMissing(t, recoveryCase{backend: backend, operation: operation}, wrapped)
				})
			}
		}
	}
}
func checkRecoveryMissing(t *testing.T, item recoveryCase, wrapped bool) {
	t.Helper()
	f := newRecoveryFixture(t, item)
	ctx := context.Background()
	if err := f.local.Store.DeleteObject(ctx, "probe", "target"); err != nil {
		t.Fatal(err)
	}
	f.local.failure = storage.ErrObjectNotFound
	if wrapped {
		f.local.failure = fmt.Errorf("lookup: %w", f.local.failure)
	}
	if err := f.adapter.RecoverPrepared(ctx); err != nil {
		t.Fatalf("reliable missing rejected: %v", err)
	}
	wantPending := 0
	if f.entry.Operation == runthrough.OutboxDelete {
		wantPending = 1
	}
	assertRecoveryConfirmed(t, f, wantPending)
	f.reopen(t)
	f.local.failure = nil
	if err := f.adapter.RetryPending(ctx); err != nil {
		t.Fatal(err)
	}
	if f.upstream.putCalls != 0 || f.upstream.delCalls != wantPending {
		t.Errorf("reliable missing replay changed: puts=%d deletes=%d", f.upstream.putCalls, f.upstream.delCalls)
	}
	if len(f.outbox.Prepared()) != 0 || len(f.outbox.Pending()) != 0 {
		t.Error("completed recovery retained intent")
	}
	_, err := f.local.Store.HeadObject(ctx, "probe", "target")
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("local absence changed: %v", err)
	}
}

func assertRecoveryConfirmed(t *testing.T, f *recoveryFixture, wantPending int) {
	t.Helper()
	pending := f.outbox.Pending()
	if len(f.outbox.Prepared()) != 0 || len(pending) != wantPending {
		t.Fatalf("reliable missing changed: prepared=%+v pending=%+v", f.outbox.Prepared(), pending)
	}
	if wantPending == 1 && pending[0].Version != f.entry.PreviousVersion {
		t.Error("delete lost its original version")
	}
}
