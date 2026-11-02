package runthrough_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type openingCapture struct {
	file          *os.File
	reads, closes int
	failed        bool
	closeErr      error
}

func (c *openingCapture) Read(p []byte) (int, error) { c.reads++; return c.file.Read(p) }
func (c *openingCapture) Close() error {
	c.closes++
	err := c.file.Close()
	if c.closeErr != nil {
		return c.closeErr
	}
	return err
}

type openingStore struct {
	*storage.MemoryStore
	path              string
	gets, faultAt     int
	failure, closeErr error
	nilReader         bool
	captures          []*openingCapture
}

func (s *openingStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	s.gets++
	meta, err := s.MemoryStore.HeadObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	failed := s.gets == s.faultAt && s.failure != nil
	if failed && s.nilReader {
		return nil, nil, s.failure
	}
	file, err := os.Open(s.path)
	if err != nil {
		return nil, nil, err
	}
	capture := &openingCapture{file: file, failed: failed}
	s.captures = append(s.captures, capture)
	if failed {
		capture.closeErr = s.closeErr
		return capture, meta, s.failure
	}
	return capture, meta, nil
}

type openingClient struct {
	*mockUpstream
	store     *openingStore
	beforeGet func()
}

func (c *openingClient) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	if c.beforeGet != nil {
		c.beforeGet()
	}
	return c.store.GetObject(ctx, bucket, key)
}

type openingCase struct{ path, mode, variant string }
type openingFixture struct {
	adapter                *runthrough.Adapter
	instance               *runtime.Instance
	local, cache, upstream *openingStore
	failure                error
	client                 *openingClient
}

func newOpeningStore(t testing.TB) *openingStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &openingStore{MemoryStore: storage.NewMemoryStore(), path: path}
	if err := s.CreateBucket(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, capture := range s.captures {
			_ = capture.file.Close()
		}
		_ = s.MemoryStore.Close()
	})
	return s
}
func seedOpeningStore(t testing.TB, s *openingStore) {
	t.Helper()
	_, err := s.PutObject(context.Background(), "probe", "target", strings.NewReader("old"), storage.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "original"}})
	if err != nil {
		t.Fatal(err)
	}
}
func newOpeningFixture(t testing.TB, item openingCase) *openingFixture {
	t.Helper()
	f := &openingFixture{local: newOpeningStore(t), cache: newOpeningStore(t), upstream: newOpeningStore(t), failure: errors.New("provider opening failed")}
	seedOpeningStore(t, f.upstream)
	var fault *openingStore
	switch item.path {
	case "local":
		seedOpeningStore(t, f.local)
		fault = f.local
	case "cached":
		seedOpeningStore(t, f.cache)
		fault = f.cache
	case "upstream":
		fault = f.upstream
	default:
		fault = f.cache
	}
	fault.faultAt = 1
	fault.failure = f.failure
	if item.path == "retry" || item.path == "retry-success" {
		fault.failure = storage.ErrObjectNotFound
	}
	if item.path == "retry" {
		f.upstream.faultAt = 2
		f.upstream.failure = f.failure
	}
	configureOpeningFault(f, item.variant)
	f.client = &openingClient{mockUpstream: newMockUpstream(), store: f.upstream}
	f.adapter = runthrough.NewWithCache(runthrough.Config{Policy: runthrough.PolicyReadThroughCache}, f.local, f.cache, f.client)
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendMemory}, f.adapter, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.instance = instance
	t.Cleanup(func() { _ = instance.Close() })
	return f
}
func configureOpeningFault(f *openingFixture, variant string) {
	for _, s := range []*openingStore{f.local, f.cache, f.upstream} {
		s.nilReader = variant == "nil"
		if variant == "close-error" {
			s.closeErr = errors.Join(storage.ErrMutationCommitted, storage.ErrObjectNotFound)
		}
	}
}

func observeOpeningFixture(f *openingFixture, mode string) (io.ReadCloser, *storage.ObjectMeta, error) {
	ctx := context.Background()
	switch mode {
	case "runtime":
		reader, object, err := f.instance.OpenObject(ctx, "probe", "target")
		if err != nil {
			return reader, nil, err
		}
		return reader, &storage.ObjectMeta{Size: object.Size, ETag: object.ETag, ContentType: object.ContentType, Metadata: object.Metadata}, nil
	case "head":
		meta, err := f.adapter.HeadObject(ctx, "probe", "target")
		return nil, meta, err
	default:
		return f.adapter.GetObject(ctx, "probe", "target")
	}
}
func assertOpeningCaptures(t testing.TB, f *openingFixture) {
	t.Helper()
	for _, s := range []*openingStore{f.local, f.cache, f.upstream} {
		assertStoreOpeningCaptures(t, s)
	}
}
func assertStoreOpeningCaptures(t testing.TB, s *openingStore) {
	t.Helper()
	for _, capture := range s.captures {
		if capture.closes != 1 {
			t.Errorf("reader failed=%t closes=%d want=1", capture.failed, capture.closes)
		}
		if _, err := capture.file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("reader failed=%t file still open: %v", capture.failed, err)
		}
		if capture.failed && capture.reads != 0 {
			t.Errorf("failed reader consumed payload: reads=%d", capture.reads)
		}
	}
}

func repairOpeningFixture(f *openingFixture) {
	for _, s := range []*openingStore{f.local, f.cache, f.upstream} {
		s.failure = nil
		s.closeErr = nil
		s.nilReader = false
	}
}
