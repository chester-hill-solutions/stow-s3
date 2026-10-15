package stow_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type missingAwareAbortStore struct {
	*abortPublicationStore
	attempts int
}

func (s *missingAwareAbortStore) AbortMultipartUpload(ctx context.Context, id string) error {
	s.attempts++
	if _, err := s.GetMultipartUpload(ctx, id); err != nil {
		return err
	}
	return s.abortPublicationStore.AbortMultipartUpload(ctx, id)
}

func newCustomAbortRetryFixture(t *testing.T, exists bool) (*abortFixture, *missingAwareAbortStore) {
	t.Helper()
	base := &abortPublicationStore{initiationPublicationStore: &initiationPublicationStore{partPublicationStore: &partPublicationStore{singlePartPublicationStore: newSinglePartPublicationStore()}}}
	store := &missingAwareAbortStore{abortPublicationStore: base}
	outer, err := stow.Open(stow.Options{Store: store, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outer.Close() })
	if err := outer.CreateBucket(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	if exists {
		if _, err := outer.PutObject(context.Background(), "probe", "target", []byte("old"), stow.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return &abortFixture{store: base, outer: outer, inner: embedded.UnwrapInstance(outer), exists: exists}, store
}

func TestCustomAbortRetryAccounting(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("exists=%t/wrapped=%t", exists, wrapped), func(t *testing.T) { checkCustomAbortRetry(t, exists, wrapped) })
		}
	}
}

func checkCustomAbortRetry(t *testing.T, exists, wrapped bool) {
	t.Helper()
	ctx := context.Background()
	f, store := newCustomAbortRetryFixture(t, exists)
	id := customAbortUpload(t, f)
	f.store.abortErr = errors.New("abort acknowledgement unavailable")
	if err := f.inner.AbortMultipartUpload(ctx, id); err != f.store.abortErr {
		t.Fatal(err)
	}
	assertCustomAbortGone(t, f)
	if _, err := f.outer.PutObject(ctx, "probe", "target", []byte("12345"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("uncertain abort released bytes prematurely: %v", err)
	}
	if _, err := f.inner.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{}); !errors.Is(err, stowruntime.ErrQuotaExceeded) {
		t.Fatalf("uncertain abort released session prematurely: %v", err)
	}
	if wrapped {
		f.store.missing = fmt.Errorf("abort: %w", stow.ErrObjectNotFound)
	}
	if err := f.inner.AbortMultipartUpload(ctx, id); err != storage.ErrUploadNotFound {
		t.Errorf("retry error=%v want internal missing upload", err)
	}
	attempts := store.attempts
	if err := f.inner.AbortMultipartUpload(ctx, id); err != storage.ErrUploadNotFound || store.attempts != attempts {
		t.Errorf("repeat error=%v provider attempts=%d", err, store.attempts-attempts)
	}
	assertCustomAbortGone(t, f)
	checkCustomAbortCapacity(t, f)
}
