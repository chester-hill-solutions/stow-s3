package storage_test

// Copy coherence and copy conditions, as a property of every backend rather than a
// fact about one of them.
//
// The concerns are separate from the rest of the contract suite because they are
// about *when* a copy happened rather than about what it contains, and a backend
// that copies a read and a later write can satisfy every other case here.

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The two complete versions the copy-coherence cases race over.
const (
	firstVersion  = "aaaa-first-version"
	secondVersion = "bbbbbbbb-second-version"
)

// A copy publishes one version of the source, and a copy's bytes and metadata
// always describe that same version.
//
// Each interleaving below is a race, so none is a reliable failure on its own —
// which is exactly why the assertion is a property checked after the fact, and
// why it is repeated. A copy composed of a read and a later write mixes a body
// from one version with a content type and metadata from another, and the result
// is neither version of anything.
func TestStoreCopyPublishesOneCoherentVersion(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		for _, bucket := range []string{"copy", "elsewhere"} {
			if err := store.CreateBucket(ctx, bucket); err != nil {
				t.Fatalf("create bucket %s: %v", bucket, err)
			}
		}
		seedVersion(t, store, "first")

		for attempt := 0; attempt < 20; attempt++ {
			// Copies in flight while the source is overwritten and deleted, in
			// both directions between two buckets, plus one taken from a key onto
			// itself.
			var wg sync.WaitGroup
			for _, key := range []string{"copy-a", "copy-b", "copy-c"} {
				wg.Add(1)
				go func(dstKey string) {
					defer wg.Done()
					_, _ = store.CopyObject(ctx, "copy", "source", "copy", dstKey)
				}(key)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = store.CopyObject(ctx, "copy", "source", "copy", "source")
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = store.CopyObject(ctx, "elsewhere", "target", "copy", "reverse")
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = store.PutObject(ctx, "copy", "source", strings.NewReader(secondVersion), storage.PutOptions{
					ContentType: "text/plain",
					Metadata:    map[string]string{"x-amz-meta-version": "second"},
				})
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = store.DeleteObject(ctx, "copy", "source")
			}()
			wg.Wait()

			for _, key := range []string{"copy-a", "copy-b", "copy-c"} {
				assertCopyIsOneVersion(t, store, key)
			}
			// Restored, so the next attempt starts from a known state rather than
			// from a key that happens to be missing.
			seedVersion(t, store, "first")
		}
	})
}

// seedVersion writes one of the two versions of the copy-race source.
func seedVersion(t *testing.T, store storage.Store, version string) {
	t.Helper()
	body, contentType := firstVersion, "text/plain"
	if version == "second" {
		body = secondVersion
	}
	_, err := store.PutObject(context.Background(), "copy", "source", strings.NewReader(body), storage.PutOptions{
		ContentType: contentType,
		Metadata:    map[string]string{"x-amz-meta-version": version},
	})
	if err != nil {
		t.Fatalf("seed version %s: %v", version, err)
	}
}

// assertCopyIsOneVersion checks that a key is either absent or a whole version:
// the body, the size and the metadata all agree about which version it is.
func assertCopyIsOneVersion(t *testing.T, store storage.Store, key string) {
	t.Helper()
	ctx := context.Background()
	rc, meta, err := store.GetObject(ctx, "copy", key)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return
	}
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	if int64(len(body)) != meta.Size {
		t.Fatalf("%s: body is %d bytes and the metadata says %d", key, len(body), meta.Size)
	}
	version := meta.Metadata["x-amz-meta-version"]
	if !((version == "first" && string(body) == firstVersion) || (version == "second" && string(body) == secondVersion)) {
		t.Fatalf("%s: metadata says version %q and the body is %q; a copy must be one version, not a mixture", key, version, body)
	}
}

// A conditional copy refuses to publish a source version the caller did not ask
// for.
//
// This is the case where "one version" is a promise about *which* one. Checking
// the condition in the caller and then copying opens a window in which the
// source changes, and the copy then publishes a version the caller never saw
// and never agreed to. Handing the condition to the store closes the window, so
// the condition and the copied bytes are the same version by construction.
func TestStoreCopyConditionsAreEvaluatedOnTheCopiedVersion(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		if err := store.CreateBucket(ctx, "copy"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		original, err := store.PutObject(ctx, "copy", "source", strings.NewReader(firstVersion), storage.PutOptions{
			ContentType: "text/plain",
			Metadata:    map[string]string{"x-amz-meta-version": "first"},
		})
		if err != nil {
			t.Fatalf("put source: %v", err)
		}
		if _, err := store.PutObject(ctx, "copy", "source", strings.NewReader(secondVersion), storage.PutOptions{
			ContentType: "text/plain",
			Metadata:    map[string]string{"x-amz-meta-version": "second"},
		}); err != nil {
			t.Fatalf("overwrite source: %v", err)
		}

		conditional, ok := store.(storage.ConditionalCopyStore)
		if !ok {
			t.Fatalf("%T does not implement storage.ConditionalCopyStore, so a conditional copy cannot be evaluated on the version it copies", store)
		}
		copyTo := func(dstKey string, opts storage.CopyOptions) error {
			_, err := conditional.CopyObjectCond(ctx, storage.CopyRequest{
				SourceBucket: "copy",
				SourceKey:    "source",
				DestBucket:   "copy",
				DestKey:      dstKey,
				Options:      opts,
			})
			return err
		}

		// A condition naming the version that is no longer there refuses, rather
		// than copying whatever is there now.
		if err := copyTo("target", storage.CopyOptions{SourceIfMatch: original.ETag}); !errors.Is(err, storage.ErrPreconditionFailed) {
			t.Fatalf("copy with a stale If-Match = %v, want ErrPreconditionFailed", err)
		}
		current, err := store.HeadObject(ctx, "copy", "source")
		if err != nil {
			t.Fatalf("head source: %v", err)
		}
		// A condition naming the version that is there succeeds, and what lands
		// is that version.
		if err := copyTo("target", storage.CopyOptions{SourceIfMatch: current.ETag}); err != nil {
			t.Fatalf("copy with a current If-Match: %v", err)
		}
		assertCopyIsOneVersion(t, store, "target")
		copied, err := store.HeadObject(ctx, "copy", "target")
		if err != nil {
			t.Fatalf("head target: %v", err)
		}
		if copied.Metadata["x-amz-meta-version"] != "second" {
			t.Fatalf("copied version = %q, want second", copied.Metadata["x-amz-meta-version"])
		}
		// If-None-Match is the same rule from the other side.
		if err := copyTo("other", storage.CopyOptions{SourceIfNoneMatch: current.ETag}); !errors.Is(err, storage.ErrPreconditionFailed) {
			t.Fatalf("copy with a matching If-None-Match = %v, want ErrPreconditionFailed", err)
		}
		// A source deleted before the copy is reported as gone, not published as
		// an empty object.
		if err := store.DeleteObject(ctx, "copy", "source"); err != nil {
			t.Fatalf("delete source: %v", err)
		}
		if _, err := store.CopyObject(ctx, "copy", "source", "copy", "gone"); !errors.Is(err, storage.ErrObjectNotFound) {
			t.Fatalf("copy of a deleted source = %v, want ErrObjectNotFound", err)
		}
	})
}
