package runthrough_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

// This exercises the production shape: runtime quota preflight wraps an
// adapter with separate authoritative and cache stores, and the cache begins
// without even a bucket. Adapter-only tests that pre-seed both stores missed
// this exact startup state.
func TestRuntimeWithSeparateEmptyCacheReadsThroughAndAcceptsLocalWrite(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	cache := storage.NewMemoryStore()
	upstream := newMockUpstream()
	if _, err := upstream.PutObject(ctx, "bucket", "remote.txt", strings.NewReader("remote"), storage.PutOptions{}); err != nil {
		t.Fatalf("seed upstream: %v", err)
	}

	adapter := runthrough.NewWithCache(runthrough.Config{
		Policy:     runthrough.PolicyReadThroughCache,
		Revalidate: false,
	}, local, cache, upstream)
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendMemory}, adapter, nil)
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	api, err := runtime.NewStoreAdapter(instance)
	if err != nil {
		t.Fatalf("make runtime adapter: %v", err)
	}
	if err := api.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create local namespace bucket: %v", err)
	}
	assertRuntimeListIncludesUpstream(t, instance)
	assertShadowedBucketReads(t, instance, upstream, cache)
	assertLocalWriteAfterEmptyCachePreflight(t, instance, upstream)
}

func TestRuntimeCompositionHeadsReadsAndPaginatesWithEmptyCache(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	cache := storage.NewMemoryStore()
	upstream := newMockUpstream()
	for key, body := range map[string]string{"a.txt": "alpha", "b.txt": "bravo", "c.txt": "charlie"} {
		if _, err := upstream.PutObject(ctx, "bucket", key, strings.NewReader(body), storage.PutOptions{}); err != nil {
			t.Fatalf("seed upstream %s: %v", key, err)
		}
	}
	adapter := runthrough.NewWithCache(runthrough.Config{Policy: runthrough.PolicyReadThroughCache}, local, cache, upstream)
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendMemory}, adapter, nil)
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create local namespace bucket: %v", err)
	}

	assertRuntimeHeadAndCachedGet(t, instance, upstream)
	assertRuntimeUpstreamPagination(t, instance, upstream)
	if _, err := instance.GetObject(ctx, "upstream-only", "a.txt"); !errors.Is(err, storage.ErrBucketNotFound) {
		t.Fatalf("read of unshadowed upstream bucket = %v, want local namespace refusal", err)
	}
	if upstream.getCalls != 1 {
		t.Fatalf("unshadowed bucket reached upstream; get calls = %d", upstream.getCalls)
	}
}

func assertRuntimeHeadAndCachedGet(t *testing.T, instance *runtime.Instance, upstream *mockUpstream) {
	t.Helper()
	ctx := context.Background()
	meta, err := instance.HeadObject(ctx, "bucket", "a.txt")
	if err != nil || meta.Size != int64(len("alpha")) {
		t.Fatalf("head through empty cache = (%+v, %v)", meta, err)
	}
	if upstream.getCalls != 1 {
		t.Fatalf("head cache miss used %d upstream gets, want 1", upstream.getCalls)
	}
	got, err := instance.GetObject(ctx, "bucket", "a.txt")
	if err != nil || string(got.Data) != "alpha" {
		t.Fatalf("get cached object = (%q, %v)", got.Data, err)
	}
	if upstream.getCalls != 1 {
		t.Fatalf("get after head used %d upstream gets, want cached result", upstream.getCalls)
	}

	if upstream.getCalls != 1 {
		t.Fatalf("get after head used %d upstream gets, want cached result", upstream.getCalls)
	}
}

func assertRuntimeUpstreamPagination(t *testing.T, instance *runtime.Instance, upstream *mockUpstream) {
	t.Helper()
	ctx := context.Background()
	page, err := instance.ListObjects(ctx, "bucket", runtime.ListOptions{Limit: 1})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Key != "a.txt" || !page.Truncated {
		t.Fatalf("first upstream page = (%+v, %v)", page, err)
	}
	page, err = instance.ListObjects(ctx, "bucket", runtime.ListOptions{Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Key != "b.txt" {
		t.Fatalf("second upstream page = (%+v, %v)", page, err)
	}
	if upstream.listCalls < 2 {
		t.Fatalf("upstream list calls = %d, want both pages", upstream.listCalls)
	}
}

func TestRuntimeCompositionPropagatesPutDeleteCopyAndMultipartThroughDurableOutbox(t *testing.T) {
	dir := t.TempDir()
	local, err := fs.NewFilesystemStore(filepath.Join(dir, "local"))
	if err != nil {
		t.Fatalf("open local store: %v", err)
	}
	cache, err := fs.NewFilesystemStore(filepath.Join(dir, "cache"))
	if err != nil {
		_ = local.Close()
		t.Fatalf("open cache store: %v", err)
	}
	upstream := newMockUpstream()
	outbox, err := runthrough.NewFileOutbox(filepath.Join(dir, "outbox.json"))
	if err != nil {
		_ = local.Close()
		_ = cache.Close()
		t.Fatalf("open durable outbox: %v", err)
	}
	adapter := runthrough.NewWithOutbox(runthrough.Config{
		Policy:          runthrough.PolicyMirrorWrites,
		AllowLiveWrites: true,
	}, local, cache, upstream, outbox)
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendFilesystem, MaxBytes: 1 << 20, MaxObjects: 100}, adapter, nil)
	if err != nil {
		_ = adapter.Close()
		t.Fatalf("open runtime composition: %v", err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	createRuntimeBuckets(t, instance, "bucket", "destination")
	exerciseRuntimeMutations(t, instance)
	assertUpstreamMutationResults(t, upstream)
}

func TestRuntimeCompositionRetriesDurableWriteAfterAdapterRestart(t *testing.T) {
	dir := t.TempDir()
	upstream := newMockUpstream()
	instance, adapter := openDurableRunThroughRuntime(t, dir, upstream)
	if err := instance.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatalf("create local bucket: %v", err)
	}
	upstream.putErrors[objectKey("bucket", "retry.txt")] = runthrough.NewTransientUpstreamError(errors.New("temporary outage"))
	if _, err := instance.PutObject(context.Background(), "bucket", "retry.txt", []byte("recover me"), runtime.PutOptions{}); err == nil {
		t.Fatal("runtime put should report the transient propagation failure")
	}
	entries := adapter.OutboxEntries()
	if len(entries) != 1 || entries[0].Terminal || entries[0].Key != "retry.txt" {
		t.Fatalf("durable outbox after failed propagation = %+v", entries)
	}
	if err := instance.Close(); err != nil {
		t.Fatalf("close first runtime: %v", err)
	}
	delete(upstream.putErrors, objectKey("bucket", "retry.txt"))
	time.Sleep(1100 * time.Millisecond)

	restarted, restartedAdapter := openDurableRunThroughRuntime(t, dir, upstream)
	defer restarted.Close()
	if err := restartedAdapter.RecoverPrepared(context.Background()); err != nil {
		t.Fatalf("recover prepared intents after restart: %v", err)
	}
	if err := restartedAdapter.RetryPending(context.Background()); err != nil {
		t.Fatalf("retry pending intent after restart: %v", err)
	}
	body, _, err := upstream.GetObject(context.Background(), "bucket", "retry.txt")
	if err != nil {
		t.Fatalf("retried object missing upstream: %v", err)
	}
	data, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || string(data) != "recover me" {
		t.Fatalf("retried upstream body = (%q, %v)", data, err)
	}
}

func openDurableRunThroughRuntime(t *testing.T, dir string, upstream *mockUpstream) (*runtime.Instance, *runthrough.Adapter) {
	t.Helper()
	local, err := fs.NewFilesystemStore(filepath.Join(dir, "local"))
	if err != nil {
		t.Fatalf("open persistent local store: %v", err)
	}
	cache, err := fs.NewFilesystemStore(filepath.Join(dir, "cache"))
	if err != nil {
		_ = local.Close()
		t.Fatalf("open persistent cache store: %v", err)
	}
	outbox, err := runthrough.NewFileOutbox(filepath.Join(dir, "outbox.json"))
	if err != nil {
		_ = local.Close()
		_ = cache.Close()
		t.Fatalf("open persistent outbox: %v", err)
	}
	adapter := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, local, cache, upstream, outbox)
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendFilesystem, MaxBytes: 1 << 20, MaxObjects: 100}, adapter, nil)
	if err != nil {
		_ = adapter.Close()
		t.Fatalf("open run-through runtime: %v", err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	return instance, adapter
}

func createRuntimeBuckets(t *testing.T, instance *runtime.Instance, buckets ...string) {
	t.Helper()
	for _, bucket := range buckets {
		if err := instance.CreateBucket(context.Background(), bucket); err != nil {
			t.Fatalf("create local bucket %s: %v", bucket, err)
		}
	}
}

func exerciseRuntimeMutations(t *testing.T, instance *runtime.Instance) {
	t.Helper()
	ctx := context.Background()
	if _, err := instance.PutObject(ctx, "bucket", "source.txt", []byte("source"), runtime.PutOptions{}); err != nil {
		t.Fatalf("runtime put: %v", err)
	}
	if _, err := instance.CopyObject(ctx, "bucket", "source.txt", "destination", "copy.txt"); err != nil {
		t.Fatalf("runtime copy: %v", err)
	}
	if err := instance.DeleteObject(ctx, "bucket", "source.txt"); err != nil {
		t.Fatalf("runtime delete: %v", err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "bucket", "multipart.bin", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create multipart upload: %v", err)
	}
	part, err := instance.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("part")))
	if err != nil {
		t.Fatalf("upload part: %v", err)
	}
	if _, err := instance.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
		t.Fatalf("complete multipart upload: %v", err)
	}
}

func assertUpstreamMutationResults(t *testing.T, upstream *mockUpstream) {
	t.Helper()
	ctx := context.Background()
	for key, want := range map[string]string{"destination/copy.txt": "source", "bucket/multipart.bin": "part"} {
		bucket, objectKey, _ := strings.Cut(key, "/")
		body, _, err := upstream.GetObject(ctx, bucket, objectKey)
		if err != nil {
			t.Fatalf("upstream get %s: %v", key, err)
		}
		data, readErr := io.ReadAll(body)
		_ = body.Close()
		if readErr != nil || string(data) != want {
			t.Fatalf("upstream object %s = (%q, %v), want %q", key, data, readErr, want)
		}
	}
	if _, err := upstream.HeadObject(ctx, "bucket", "source.txt"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("deleted source remains upstream: %v", err)
	}
}

func assertRuntimeListIncludesUpstream(t *testing.T, instance *runtime.Instance) {
	t.Helper()
	listed, err := instance.ListObjects(context.Background(), "bucket", runtime.ListOptions{})
	if err != nil || len(listed.Objects) != 1 || listed.Objects[0].Key != "remote.txt" {
		t.Fatalf("runtime list with an empty cache = (%+v, %v), want upstream object", listed.Objects, err)
	}
}

func assertShadowedBucketReads(t *testing.T, instance *runtime.Instance, upstream *mockUpstream, cache storage.Store) {
	t.Helper()
	got, err := instance.GetObject(context.Background(), "bucket", "remote.txt")
	if err != nil || string(got.Data) != "remote" {
		t.Fatalf("runtime read-through = (%q, %v), want remote bytes", got.Data, err)
	}
	if upstream.getCalls != 1 {
		t.Fatalf("upstream get calls = %d, want 1", upstream.getCalls)
	}
	if _, err := instance.GetObject(context.Background(), "upstream-only", "remote.txt"); !errors.Is(err, storage.ErrBucketNotFound) {
		t.Fatalf("unshadowed upstream bucket error = %v, want local namespace refusal", err)
	}
	if upstream.getCalls != 1 {
		t.Fatalf("unshadowed bucket reached upstream; get calls = %d", upstream.getCalls)
	}
	if _, err := cache.HeadObject(context.Background(), "bucket", "remote.txt"); err != nil {
		t.Fatalf("cache was not lazily materialized: %v", err)
	}
}

func assertLocalWriteAfterEmptyCachePreflight(t *testing.T, instance *runtime.Instance, upstream *mockUpstream) {
	t.Helper()
	// HeadObject is the runtime's quota preflight. An absent object in the
	// absent cache bucket must be interpreted as absent, allowing a local-only
	// write to proceed without an upstream mutation.
	upstreamWritesBefore := upstream.putCalls
	if _, err := instance.PutObject(context.Background(), "bucket", "local.txt", []byte("local"), runtime.PutOptions{}); err != nil {
		t.Fatalf("runtime put through empty-cache preflight: %v", err)
	}
	if upstream.putCalls != upstreamWritesBefore {
		t.Fatalf("local-authority policy made %d additional upstream writes, want zero", upstream.putCalls-upstreamWritesBefore)
	}
}
