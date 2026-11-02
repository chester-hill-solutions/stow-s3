package runthrough_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type propagationNoMetaStore struct{ *openingStore }

func (s *propagationNoMetaStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	reader, meta, err := s.openingStore.GetObject(ctx, bucket, key)
	if err != nil {
		return reader, nil, err
	}
	return reader, meta, nil
}

type propagationOpenCase struct {
	action, variant string
	marked          bool
}
type propagationOpenFixture struct {
	local    *openingStore
	upstream *mockUpstream
	outbox   *runthrough.FileOutbox
	adapter  *runthrough.Adapter
	failure  error
}

func newPropagationOpenFixture(t *testing.T, item propagationOpenCase) *propagationOpenFixture {
	t.Helper()
	local := newOpeningStore(t)
	seedOpeningStore(t, local)
	failure := errors.New("source opening refused")
	if item.marked {
		failure = errors.Join(storage.ErrMutationCommitted, failure)
	}
	local.faultAt = 1
	local.failure = failure
	local.nilReader = item.variant == "nil"
	if item.variant == "close-error" {
		local.closeErr = errors.Join(errors.New("cleanup-only"), runthrough.ErrOutboxVersionConflict, storage.ErrObjectNotFound)
	}
	var store storage.Store = local
	if item.variant == "no-meta" {
		store = &propagationNoMetaStore{local}
	}
	outbox := fileVersionOutbox(t)
	upstream := newMockUpstream()
	adapter := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, store, storage.NewMemoryStore(), upstream, outbox)
	t.Cleanup(func() { _ = adapter.Close() })
	return &propagationOpenFixture{local, upstream, outbox, adapter, failure}
}
func startPropagationOpening(f *propagationOpenFixture, action string) error {
	ctx := context.Background()
	if action == "live-put" {
		_, err := f.adapter.PutObject(ctx, "probe", "target", strings.NewReader("old"), storage.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}})
		return err
	}
	operation := map[string]runthrough.OutboxOperation{"queued-put": runthrough.OutboxPut, "queued-copy": runthrough.OutboxCopy, "queued-multipart": runthrough.OutboxMultipart}[action]
	meta, err := f.local.HeadObject(ctx, "probe", "target")
	if err != nil {
		return err
	}
	_, err = f.outbox.Enqueue(runthrough.OutboxEntry{Operation: operation, Bucket: "probe", Key: "target", Version: meta.VersionID, UpstreamAbsent: true, SourceBucket: "probe", SourceKey: "source"})
	if err != nil {
		return err
	}
	return f.adapter.RetryPending(ctx)
}
func TestPropagationOpeningFailureClosesSource(t *testing.T) {
	for _, action := range []string{"live-put", "queued-put", "queued-copy", "queued-multipart"} {
		for _, variant := range []string{"reader", "nil", "close-error", "no-meta"} {
			for _, marked := range []bool{false, true} {
				item := propagationOpenCase{action, variant, marked}
				t.Run(fmt.Sprintf("%s/%s/marked=%t", action, variant, marked), func(t *testing.T) { checkPropagationOpening(t, item) })
			}
		}
	}
}
func checkPropagationOpening(t *testing.T, item propagationOpenCase) {
	t.Helper()
	f := newPropagationOpenFixture(t, item)
	err := startPropagationOpening(f, item.action)
	assertPropagationOpeningError(t, f, item, err)
	assertStoreOpeningCaptures(t, f.local)
	if f.upstream.putCalls != 0 {
		t.Error("failed opening reached upstream PUT")
	}
	pending := f.outbox.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending=%+v", pending)
	}
	entry := pending[0]
	if entry.Terminal || entry.Attempts != 1 || entry.NextAttempt.IsZero() || entry.LastError != f.failure.Error() {
		t.Errorf("retry classification changed: %+v", entry)
	}
	localMeta, headErr := f.local.HeadObject(context.Background(), "probe", "target")
	if headErr != nil || localMeta.VersionID != entry.Version {
		t.Fatalf("committed local version lost: meta=%+v entry=%+v error=%v", localMeta, entry, headErr)
	}
	f.local.failure = nil
	f.local.closeErr = nil
	f.local.nilReader = false
	if err := f.outbox.MarkFailure(entry.ID, f.failure, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.RetryPending(context.Background()); err != nil {
		t.Fatalf("repaired retry failed: %v", err)
	}
	if len(f.outbox.Pending()) != 0 || f.upstream.putCalls != 1 {
		t.Errorf("retry did not finish exactly once: pending=%+v upstream puts=%d", f.outbox.Pending(), f.upstream.putCalls)
	}
	assertStoreOpeningCaptures(t, f.local)
}
func TestPropagationVersionConflictClosesSourceOnce(t *testing.T) {
	f := newPropagationOpenFixture(t, propagationOpenCase{})
	f.local.failure = nil
	_, err := f.outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "probe", Key: "target", Version: "wrong-version", UpstreamAbsent: true})
	if err != nil {
		t.Fatal(err)
	}
	err = f.adapter.RetryPending(context.Background())
	if !errors.Is(err, runthrough.ErrOutboxVersionConflict) {
		t.Fatalf("version conflict changed: %v", err)
	}
	assertStoreOpeningCaptures(t, f.local)
	if f.upstream.putCalls != 0 || f.local.captures[0].reads != 0 {
		t.Fatal("version conflict sent a body upstream")
	}
	pending := f.outbox.Pending()
	if len(pending) != 1 || !pending[0].Terminal {
		t.Fatalf("conflict classification changed: %+v", pending)
	}
}

func assertPropagationOpeningError(t *testing.T, f *propagationOpenFixture, item propagationOpenCase, err error) {
	t.Helper()
	if !errors.Is(err, f.failure) {
		t.Fatalf("source error changed: got=%v want=%v", err, f.failure)
	}
	wantCommitted := item.action == "live-put" || item.marked
	if errors.Is(err, storage.ErrMutationCommitted) != wantCommitted {
		t.Errorf("commit status changed: %v", err)
	}
	if strings.Contains(err.Error(), "cleanup-only") || errors.Is(err, runthrough.ErrOutboxVersionConflict) {
		t.Errorf("cleanup replaced source failure: %v", err)
	}
}
