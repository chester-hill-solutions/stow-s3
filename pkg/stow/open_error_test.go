package stow_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type openingFailureStore struct {
	*lazyStore
	failure    error
	closeErr   error
	closeCalls int
	readErr    error
}

func (s *openingFailureStore) OpenObject(ctx context.Context, bucket, key string) (io.ReadCloser, stow.Object, error) {
	reader, object, err := s.lazyStore.OpenObject(ctx, bucket, key)
	if err != nil {
		return reader, object, err
	}
	return &openingFailureReader{ReadCloser: reader, store: s}, object, s.failure
}

type openingFailureReader struct {
	io.ReadCloser
	store *openingFailureStore
}

func (r *openingFailureReader) Read(p []byte) (int, error) {
	if r.store.readErr != nil {
		return 0, r.store.readErr
	}
	return r.ReadCloser.Read(p)
}

func (r *openingFailureReader) Close() error {
	// Count every invocation, including an accidental second close.
	r.store.closeCalls++
	_ = r.ReadCloser.Close()
	return r.store.closeErr
}

type openingRangeFailureStore struct{ *openingFailureStore }

func (s *openingRangeFailureStore) OpenObjectRange(ctx context.Context, bucket, key string, _ stow.ObjectRange) (io.ReadCloser, stow.Object, error) {
	return s.OpenObject(ctx, bucket, key)
}

func TestPublicOpeningErrorClosesReader(t *testing.T) {
	for _, mode := range []string{"stream", "get", "fallback-range", "native-range"} {
		for _, closeFailure := range []bool{false, true} {
			name := mode + "/close-ok"
			if closeFailure {
				name = mode + "/close-error"
			}
			t.Run(name, func(t *testing.T) { checkPublicOpeningError(t, mode, closeFailure) })
		}
	}
}

func openFailureRuntime(t *testing.T, store *openingFailureStore, mode string) *stow.Runtime {
	t.Helper()
	var backend stow.Store = store
	if mode == "native-range" {
		backend = &openingRangeFailureStore{store}
	}
	runtime, err := stow.Open(stow.Options{Backend: stow.BackendHost, Store: backend})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime
}

func observePublicOpening(runtime *stow.Runtime, mode string) (io.ReadCloser, stow.Object, error) {
	ctx := context.Background()
	switch mode {
	case "get":
		object, err := runtime.GetObject(ctx, "assets", "document")
		return nil, object, err
	case "stream":
		return runtime.OpenObject(ctx, "assets", "document")
	default:
		return runtime.OpenObjectRange(ctx, "assets", "document", stow.ObjectRange{Length: 1})
	}
}

func checkPublicOpeningError(t *testing.T, mode string, closeFailure bool) {
	t.Helper()
	_, lazy := openLazyRuntime(t)
	failure := errors.New("public provider opening failed")
	store := &openingFailureStore{lazyStore: lazy, failure: failure}
	if closeFailure {
		store.closeErr = errors.Join(stow.ErrMutationCommitted, stow.ErrObjectNotFound)
	}
	runtime := openFailureRuntime(t, store, mode)
	reader, object, err := observePublicOpening(runtime, mode)
	if err != failure || reader != nil || object.Bucket != "" {
		t.Errorf("opening contract: reader=%v object=%+v error=%v", reader, object, err)
	}
	if store.closeCalls != 1 || lazy.closes != 1 || lazy.reads != 0 || lazy.opens != 1 {
		t.Errorf("calls=%d closes=%d reads=%d opens=%d", store.closeCalls, lazy.closes, lazy.reads, lazy.opens)
	}
	store.failure = nil
	store.closeErr = nil
	retry, err := runtime.GetObject(context.Background(), "assets", "document")
	if err != nil || string(retry.Data) != "0123456789" {
		t.Fatalf("healthy retry: object=%+v error=%v", retry, err)
	}
}

func TestPublicOpeningRangeCleanupOnce(t *testing.T) {
	for _, skipFailure := range []bool{false, true} {
		name := "invalid-range"
		if skipFailure {
			name = "skip-error"
		}
		t.Run(name, func(t *testing.T) {
			_, lazy := openLazyRuntime(t)
			store := &openingFailureStore{lazyStore: lazy}
			span := stow.ObjectRange{Offset: 10, Length: 1}
			if skipFailure {
				span.Offset = 1
				store.readErr = io.ErrUnexpectedEOF
			}
			runtime, err := stow.Open(stow.Options{Backend: stow.BackendHost, Store: store})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			reader, _, err := runtime.OpenObjectRange(context.Background(), "assets", "document", span)
			if err == nil || reader != nil || store.closeCalls != 1 || lazy.closes != 1 {
				t.Fatalf("reader=%v error=%v calls=%d closes=%d", reader, err, store.closeCalls, lazy.closes)
			}
			if skipFailure && err != io.ErrUnexpectedEOF {
				t.Fatalf("original skip error: %v", err)
			}
		})
	}
}
