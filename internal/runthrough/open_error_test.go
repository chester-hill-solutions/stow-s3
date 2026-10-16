package runthrough_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestRunthroughOpeningErrorClosesReader(t *testing.T) {
	for _, path := range []string{"local", "cached", "upstream", "reopen", "retry"} {
		for _, mode := range []string{"adapter", "runtime"} {
			for _, variant := range []string{"reader", "close-error", "nil"} {
				item := openingCase{path, mode, variant}
				t.Run(path+"/"+mode+"/"+variant, func(t *testing.T) { checkRunthroughOpeningError(t, item) })
			}
		}
	}
}
func TestRunthroughHeadOpeningErrorClosesReader(t *testing.T) {
	for _, variant := range []string{"reader", "close-error", "nil"} {
		t.Run(variant, func(t *testing.T) { checkRunthroughOpeningError(t, openingCase{"upstream", "head", variant}) })
	}
}
func checkRunthroughOpeningError(t *testing.T, item openingCase) {
	t.Helper()
	f := newOpeningFixture(t, item)
	usage := f.instance.Usage()
	reader, _, err := observeOpeningFixture(f, item.mode)
	if reader != nil {
		t.Errorf("failed opening returned a reader")
	}
	if err != f.failure {
		t.Errorf("opening error changed: got=%v want=%v", err, f.failure)
	}
	if f.instance.Usage() != usage {
		t.Error("failed read changed runtime usage")
	}
	assertOpeningCaptures(t, f)
	repairOpeningFixture(f)
	checkOpeningRetry(t, f)
}
func checkOpeningRetry(t *testing.T, f *openingFixture) {
	t.Helper()
	reader, meta, err := f.adapter.GetObject(context.Background(), "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "old" || meta.Size != 3 {
		t.Fatalf("retry=%q meta=%+v error=%v", data, meta, err)
	}
	if meta.ContentType != "text/plain" || meta.Metadata["state"] != "original" {
		t.Fatalf("retry lost properties: %+v", meta)
	}
	before, err := f.upstream.HeadObject(context.Background(), "probe", "target")
	if err != nil || meta.ETag != before.ETag {
		t.Fatalf("retry identity: got=%+v expected=%+v error=%v", meta, before, err)
	}
}
func TestRunthroughMissingCacheReaderClosedBeforeFallback(t *testing.T) {
	for _, mode := range []string{"adapter", "runtime"} {
		for _, variant := range []string{"reader", "close-error"} {
			t.Run(mode+"/"+variant, func(t *testing.T) {
				f := newOpeningFixture(t, openingCase{"retry-success", mode, variant})
				f.client.beforeGet = func() {
					if f.upstream.gets == 1 {
						assertStoreOpeningCaptures(t, f.cache)
					}
				}
				reader, meta, err := observeOpeningFixture(f, mode)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(reader)
				_ = reader.Close()
				if err != nil || string(data) != "old" || meta.Size != 3 {
					t.Fatalf("fallback=%q meta=%+v error=%v", data, meta, err)
				}
				if f.upstream.gets != 2 || f.cache.gets != 1 {
					t.Fatalf("upstream gets=%d cache gets=%d", f.upstream.gets, f.cache.gets)
				}
				assertOpeningCaptures(t, f)
			})
		}
	}
}
func TestRunthroughHeadDoesNotOpenLocalOrCachedBodies(t *testing.T) {
	for _, path := range []string{"local", "cached"} {
		t.Run(path, func(t *testing.T) {
			f := newOpeningFixture(t, openingCase{path, "head", "reader"})
			reader, meta, err := observeOpeningFixture(f, "head")
			if err != nil || reader != nil || meta.Size != 3 {
				t.Fatalf("head meta=%+v error=%v", meta, err)
			}
			if f.local.gets+f.cache.gets+f.upstream.gets != 0 {
				t.Fatal("metadata-only hit opened a body")
			}
		})
	}
}
func TestRunthroughCommittedOpeningErrorPreserved(t *testing.T) {
	f := newOpeningFixture(t, openingCase{"upstream", "adapter", "close-error"})
	failure := storage.CommittedError(storage.ErrObjectNotFound)
	f.upstream.failure = failure
	reader, meta, err := f.adapter.GetObject(context.Background(), "probe", "target")
	if err != failure || !errors.Is(err, storage.ErrMutationCommitted) || reader != nil || meta != nil {
		t.Errorf("marked failure: reader=%v meta=%v error=%v", reader, meta, err)
	}
	assertOpeningCaptures(t, f)
}
