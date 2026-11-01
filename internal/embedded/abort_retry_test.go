package embedded

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type abortResultStore struct {
	MultipartStore
	err   error
	calls int
}

func (s *abortResultStore) AbortMultipartUpload(context.Context, string) error {
	s.calls++
	return s.err
}

func TestAbortMissingTranslation(t *testing.T) {
	wrapped := fmt.Errorf("abort: %w", ErrObjectNotFound)
	unknown := errors.New("abort outcome unavailable")
	committed := errors.Join(ErrMutationCommitted, ErrObjectNotFound)
	reversed := errors.Join(ErrObjectNotFound, ErrMutationCommitted)
	for _, item := range []struct {
		name           string
		err, errorWant error
	}{
		{"success", nil, nil},
		{"missing", ErrObjectNotFound, storage.ErrUploadNotFound},
		{"wrapped-missing", wrapped, storage.ErrUploadNotFound},
		{"internal-missing", storage.ErrUploadNotFound, storage.ErrUploadNotFound},
		{"unknown", unknown, unknown},
		{"refused", ErrPreconditionFailed, ErrPreconditionFailed},
		{"committed-missing", committed, committed},
		{"missing-committed", reversed, reversed},
	} {
		t.Run(item.name, func(t *testing.T) {
			store := &abortResultStore{err: item.err}
			adapter := multipartAdapter{multi: store}
			if err := adapter.AbortMultipartUpload(context.Background(), "upload"); err != item.errorWant {
				t.Errorf("error=%v want identical %v", err, item.errorWant)
			}
			if store.calls != 1 {
				t.Fatalf("provider calls=%d", store.calls)
			}
		})
	}
}
