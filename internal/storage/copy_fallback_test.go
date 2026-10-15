package storage_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestCapturedCopyPreservesProperties(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		for _, bucket := range []string{"source-bucket", "target-bucket"} {
			if err := store.CreateBucket(ctx, bucket); err != nil {
				t.Fatal(err)
			}
		}
		for _, algorithm := range []string{"", "CRC32", "CRC32C", "CRC64NVME", "SHA1", "SHA256"} {
			t.Run("checksum="+algorithm, func(t *testing.T) {
				original, err := store.PutObject(ctx, "source-bucket", "source", strings.NewReader(firstVersion), storage.PutOptions{
					ContentType: "text/plain", Metadata: map[string]string{"generation": "one"},
					ChecksumAlgorithm: algorithm, ComputeChecksum: algorithm != "",
				})
				if err != nil {
					t.Fatal(err)
				}
				for _, target := range []struct{ bucket, key string }{{"target-bucket", "target"}, {"source-bucket", "source"}} {
					copied, err := storage.CopyObject(ctx, struct{ storage.Store }{store}, storage.CopyRequest{
						SourceBucket: "source-bucket", SourceKey: "source", DestBucket: target.bucket, DestKey: target.key,
						Options: storage.CopyOptions{SourceIfMatch: original.ETag},
					})
					if err != nil {
						t.Fatal(err)
					}
					if copied.ContentType != original.ContentType || !maps.Equal(copied.Metadata, original.Metadata) {
						t.Errorf("COPY changed source properties: %+v, want %+v", copied, original)
					}
					if copied.ChecksumAlgorithm != original.ChecksumAlgorithm || copied.ChecksumValue != original.ChecksumValue {
						t.Errorf("COPY changed checksum: %+v, want %+v", copied, original)
					}
					assertCapturedCopyBody(t, store, target.bucket, target.key, firstVersion)
				}
			})
		}
	})
}

type failingCapturedCopyStore struct {
	storage.Store
	readErr, putErr error
	closed          int
	size            int64
}

func (s *failingCapturedCopyStore) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	rc, meta, err := s.Store.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	return &failingCapturedCopyReader{ReadCloser: rc, readErr: s.readErr, closed: &s.closed}, meta, nil
}

func (s *failingCapturedCopyStore) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	if sized, ok := body.(interface{ ReadSize() int64 }); ok {
		s.size = sized.ReadSize()
	}
	if s.putErr != nil {
		return nil, s.putErr
	}
	return s.Store.PutObject(ctx, bucket, key, body, opts)
}

type failingCapturedCopyReader struct {
	io.ReadCloser
	readErr error
	closed  *int
}

func (r *failingCapturedCopyReader) Read(p []byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	return r.ReadCloser.Read(p)
}

func (r *failingCapturedCopyReader) Close() error {
	*r.closed++
	return r.ReadCloser.Close()
}

func TestCapturedCopyFailureClosesSourceAndPreservesDestination(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		if err := store.CreateBucket(ctx, "copy"); err != nil {
			t.Fatal(err)
		}
		source, err := store.PutObject(ctx, "copy", "source", strings.NewReader(firstVersion), storage.PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		broken := errors.New("injected COPY failure")
		for _, failure := range []string{"condition", "read", "put"} {
			t.Run(failure, func(t *testing.T) {
				if _, err := store.PutObject(ctx, "copy", "target", strings.NewReader("kept"), storage.PutOptions{}); err != nil {
					t.Fatal(err)
				}
				wrapper := &failingCapturedCopyStore{Store: store, size: -1}
				req := storage.CopyRequest{SourceBucket: "copy", SourceKey: "source", DestBucket: "copy", DestKey: "target", Options: storage.CopyOptions{SourceIfMatch: source.ETag}}
				wantErr := broken
				switch failure {
				case "condition":
					req.Options.SourceIfMatch = "\"different\""
					wantErr = storage.ErrPreconditionFailed
				case "read":
					wrapper.readErr = broken
				case "put":
					wrapper.putErr = broken
				}
				if _, err := storage.CopyObject(ctx, wrapper, req); !errors.Is(err, wantErr) {
					t.Errorf("COPY returned %v, want %v", err, wantErr)
				}
				if wrapper.closed != 1 {
					t.Errorf("source closed %d times, want once", wrapper.closed)
				}
				if failure != "condition" && wrapper.size != source.Size {
					t.Errorf("PUT size hint = %d, want %d", wrapper.size, source.Size)
				}
				assertCapturedCopyBody(t, store, "copy", "target", "kept")
			})
		}
	})
}

func assertCapturedCopyBody(t *testing.T, store storage.Store, bucket, key, want string) {
	t.Helper()
	rc, meta, err := store.GetObject(context.Background(), bucket, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want || meta.Size != int64(len(want)) {
		t.Errorf("COPY body=%q, size=%d, want %q", body, meta.Size, want)
	}
}

func TestCapturedCopyAcceptsMultipartSourceETag(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		if err := store.CreateBucket(ctx, "copy"); err != nil {
			t.Fatal(err)
		}
		multi := requireMultipart(t, store)
		upload, err := multi.CreateMultipartUpload(ctx, "copy", "source", storage.MultipartOptions{})
		if err != nil {
			t.Fatal(err)
		}
		body := strings.Repeat("s", int(storage.MinPartSize)) + firstVersion
		part, err := multi.UploadPart(ctx, upload.UploadID, 1, strings.NewReader(body[:storage.MinPartSize]))
		if err != nil {
			t.Fatal(err)
		}
		last, err := multi.UploadPart(ctx, upload.UploadID, 2, strings.NewReader(firstVersion))
		if err != nil {
			t.Fatal(err)
		}
		source, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part, *last})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(source.ETag, "-2") {
			t.Fatalf("source ETag is not multipart: %q", source.ETag)
		}
		copied, err := storage.CopyObject(ctx, struct{ storage.Store }{store}, storage.CopyRequest{
			SourceBucket: "copy", SourceKey: "source", DestBucket: "copy", DestKey: "target",
			Options: storage.CopyOptions{SourceIfMatch: source.ETag},
		})
		if err != nil {
			t.Fatal(err)
		}
		if want := storage.ETagForBytes([]byte(body)); copied.ETag != want {
			t.Errorf("single-part destination ETag = %q, want %q", copied.ETag, want)
		}
		assertCapturedCopyBody(t, store, "copy", "target", body)
	})
}
