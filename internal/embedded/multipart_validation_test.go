package embedded

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type validationLookupStore struct {
	MultipartStore
	upload MultipartUpload
	err    error
	calls  int
}

func (s *validationLookupStore) GetMultipartUpload(context.Context, string) (MultipartUpload, error) {
	s.calls++
	return s.upload, s.err
}

func TestMultipartValidationErrors(t *testing.T) {
	unknown := errors.New("lookup failed")
	wrapped := fmt.Errorf("lookup: %w", ErrObjectNotFound)
	committed := errors.Join(ErrMutationCommitted, ErrObjectNotFound)
	reversedCommitted := errors.Join(ErrObjectNotFound, ErrMutationCommitted)
	cases := []struct {
		name   string
		err    error
		bucket string
		key    string
		want   error
	}{
		{"success", nil, "probe", "target", nil},
		{"wrong-bucket", nil, "other", "target", storage.ErrUploadNotFound},
		{"wrong-key", nil, "probe", "other", storage.ErrUploadNotFound},
		{"missing", ErrObjectNotFound, "probe", "target", storage.ErrUploadNotFound},
		{"wrapped-missing", wrapped, "probe", "target", storage.ErrUploadNotFound},
		{"internal-missing", storage.ErrUploadNotFound, "probe", "target", storage.ErrUploadNotFound},
		{"unknown", unknown, "probe", "target", unknown},
		{"committed-missing", committed, "probe", "target", committed},
		{"missing-committed", reversedCommitted, "probe", "target", reversedCommitted},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			store := &validationLookupStore{upload: MultipartUpload{Bucket: "probe", Key: "target", Metadata: map[string]string{"state": "original"}}, err: item.err}
			adapter := multipartAdapter{multi: store}
			err := adapter.ValidateMultipartUpload(context.Background(), "upload", item.bucket, item.key)
			if err != item.want {
				t.Errorf("error=%v want identical %v", err, item.want)
			}
			if store.calls != 1 || store.upload.Metadata["state"] != "original" {
				t.Fatalf("lookup calls=%d metadata=%v", store.calls, store.upload.Metadata)
			}
		})
	}
}

func BenchmarkMultipartValidation(b *testing.B) {
	for _, entries := range []int{0, 1, 512} {
		b.Run(fmt.Sprintf("success/metadata=%d", entries), func(b *testing.B) {
			store := &validationLookupStore{upload: MultipartUpload{Bucket: "probe", Key: "target", Metadata: make(map[string]string)}}
			for n := 0; n < entries; n++ {
				store.upload.Metadata[fmt.Sprint(n)] = "value"
			}
			benchmarkMultipartValidation(b, store)
		})
	}
	b.Run("wrapped-missing", func(b *testing.B) {
		benchmarkMultipartValidation(b, &validationLookupStore{err: fmt.Errorf("lookup: %w", ErrObjectNotFound)})
	})
}

func benchmarkMultipartValidation(b *testing.B, store *validationLookupStore) {
	adapter := multipartAdapter{multi: store}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		err := adapter.ValidateMultipartUpload(context.Background(), "upload", "probe", "target")
		if (err == nil) != (store.err == nil) {
			b.Fatal(err)
		}
	}
}
