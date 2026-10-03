package runthrough_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestRetryDoesNotRetireUnappliedSameBodyMetadata(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"content-type", "metadata", "checksum"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareMetadataRetry(t, kind)
			adapter, outbox, up, changed, entry := fixture.adapter, fixture.outbox, fixture.upstream, fixture.changed, fixture.entry
			up.putErr = runthrough.NewTransientUpstreamError(errors.New("failed before applying"))
			if err := adapter.RetryPending(ctx); err == nil {
				t.Fatal("expected transient preapply failure")
			}
			up.putErr = nil
			pending := outbox.Pending()
			if len(pending) != 1 {
				t.Fatalf("pending=%+v", pending)
			}
			if err := outbox.MarkFailure(entry.ID, errors.New("retry now"), pending[0].CreatedAt); err != nil {
				t.Fatal(err)
			}
			if err := adapter.RetryPending(ctx); err != nil {
				t.Fatal(err)
			}
			remote, err := up.HeadObject(ctx, "bucket", "key")
			if err != nil {
				t.Fatal(err)
			}
			if remote.ContentType != changed.ContentType || remote.Metadata["owner"] != changed.Metadata["owner"] || remote.ChecksumAlgorithm != changed.ChecksumAlgorithm {
				t.Fatalf("intent %s retired without metadata effect: %+v", entry.ID, remote)
			}
			if len(outbox.Pending()) != 0 {
				t.Fatal("completed retry retained intent")
			}
		})
	}
}

type retryMetadataCase struct {
	adapter  *runthrough.Adapter
	outbox   *runthrough.FileOutbox
	upstream *mockUpstream
	changed  storage.PutOptions
	entry    runthrough.OutboxEntry
}

func prepareMetadataRetry(t *testing.T, kind string) retryMetadataCase {
	t.Helper()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatal(err)
	}
	old, changed := retryMetadataOptions(t, kind)
	meta, err := local.PutObject(context.Background(), "bucket", "key", strings.NewReader("same bytes"), changed)
	if err != nil {
		t.Fatal(err)
	}
	up := newMockUpstream()
	remoteETag, err := up.PutObject(context.Background(), "bucket", "key", strings.NewReader("same bytes"), old)
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outbox.Close() })
	entry, err := outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "key", Version: meta.VersionID, UpstreamVersion: remoteETag})
	if err != nil {
		t.Fatal(err)
	}
	adapter := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, local, local, up, outbox)
	return retryMetadataCase{adapter: adapter, outbox: outbox, upstream: up, changed: changed, entry: entry}
}

func retryMetadataOptions(t *testing.T, kind string) (storage.PutOptions, storage.PutOptions) {
	t.Helper()
	old := storage.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"owner": "before"}}
	changed := storage.PutOptions{ContentType: old.ContentType, Metadata: map[string]string{"owner": "before"}}
	switch kind {
	case "content-type":
		changed.ContentType = "application/json"
	case "metadata":
		changed.Metadata["owner"] = "after"
	case "checksum":
		old.ChecksumAlgorithm, changed.ChecksumAlgorithm = "SHA256", "SHA1"
		var err error
		old.ChecksumValue, err = storage.ComputeChecksum(old.ChecksumAlgorithm, []byte("same bytes"))
		if err != nil {
			t.Fatal(err)
		}
		changed.ChecksumValue, err = storage.ComputeChecksum(changed.ChecksumAlgorithm, []byte("same bytes"))
		if err != nil {
			t.Fatal(err)
		}
	}
	return old, changed
}

type refusedLocalDelete struct {
	storage.Store
	failure error
}

func (s refusedLocalDelete) DeleteObject(context.Context, string, string) error { return s.failure }

func TestPreparedDeleteFailureDiscardsProductionIntent(t *testing.T) {
	ctx := context.Background()
	base := storage.NewMemoryStore()
	if err := base.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.PutObject(ctx, "bucket", "key", strings.NewReader("original"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("local delete refused")
	local := refusedLocalDelete{Store: base, failure: failure}
	up := newMockUpstream()
	if _, err := up.PutObject(ctx, "bucket", "key", strings.NewReader("original"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	box, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	adapter := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, local, local, up, box)
	if err := adapter.DeleteObject(ctx, "bucket", "key"); !errors.Is(err, failure) {
		t.Fatalf("delete=%v", err)
	}
	if len(box.Prepared()) != 0 || len(box.Pending()) != 0 || up.delCalls != 0 {
		t.Fatalf("failed delete left intent: prepared=%+v pending=%+v calls=%d", box.Prepared(), box.Pending(), up.delCalls)
	}
	reader, _, err := base.GetObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "original" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}
