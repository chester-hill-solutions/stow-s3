package storage_test

// What a completion refuses, and what it leaves behind.
//
// Separate from the fidelity cases in the sibling file because these are all
// refusals: each one is a completion that must change nothing at all, so they are
// easier to read together than interleaved with the cases about what a
// successful completion produces.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestStoreEnforcesTheMinimumPartSize(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "uploads"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		cases := []struct {
			name       string
			sizes      []int
			wantReject bool
		}{
			{"one byte plus one byte", []int{1, 1}, true},
			{"five mebibytes plus one byte", []int{storage.MinPartSize, 1}, false},
			{"a single one-byte part", []int{1}, false},
		}
		for index, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				// A key per case, so a case that publishes an object cannot be
				// mistaken for one that did not.
				key := fmt.Sprintf("object-%d.bin", index)
				upload, err := multi.CreateMultipartUpload(ctx, "uploads", key, storage.MultipartOptions{})
				if err != nil {
					t.Fatalf("create upload: %v", err)
				}
				// Distinct filler per part, so each ETag identifies its own part.
				parts := make([]storage.PartInfo, 0, len(tc.sizes))
				for partNumber, size := range tc.sizes {
					part, err := multi.UploadPart(ctx, upload.UploadID, partNumber+1,
						bytes.NewReader(bytes.Repeat([]byte{byte('a' + partNumber)}, size)))
					if err != nil {
						t.Fatalf("upload part %d: %v", partNumber+1, err)
					}
					parts = append(parts, *part)
				}

				_, err = multi.CompleteMultipartUpload(ctx, upload.UploadID, parts)
				if tc.wantReject {
					if !errors.Is(err, storage.ErrEntityTooSmall) {
						t.Fatalf("complete = %v, want ErrEntityTooSmall", err)
					}
					// Nothing published: a refused completion leaves the
					// destination alone and the upload completable.
					if _, err := store.HeadObject(ctx, "uploads", key); !errors.Is(err, storage.ErrObjectNotFound) {
						t.Errorf("a refused completion published an object: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("complete: %v", err)
				}
				var want int64
				for _, size := range tc.sizes {
					want += int64(size)
				}
				head, err := store.HeadObject(ctx, "uploads", key)
				if err != nil {
					t.Fatalf("head after completion: %v", err)
				}
				if head.Size != want {
					t.Errorf("completed size = %d, want %d", head.Size, want)
				}
			})
		}
	})
}

// A checksum algorithm named at initiation must not make the completion fail.
//
// x-amz-checksum-algorithm on CreateMultipartUpload names the algorithm the
// client intends to use. An initiation carries no body, so no value comes with
// it, and the record is a declaration rather than a claim about the object.
//
// It used to be handed to the store as though it were a claim, and
// VerifyChecksum rightly refuses an algorithm with no value - so every
// completion of an upload that named one failed, in all three backends, with an
// error the S3 surface has no mapping for. The client's answer was 500
// InternalError and the SDK retried it three times.
//
// This is here rather than in the fidelity file because that is how it presented:
// a completion that cannot happen at all. The declaration still has to be
// recorded, so the second half asserts the algorithm survives on the upload.
func TestStoreCompletesAnUploadThatDeclaredAChecksumAlgorithm(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "declared"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		body := []byte("hello")

		upload, err := multi.CreateMultipartUpload(ctx, "declared", "object.bin", storage.MultipartOptions{
			ChecksumAlgorithm: "CRC32",
		})
		if err != nil {
			t.Fatalf("create multipart upload: %v", err)
		}

		// Read while the upload is still in flight, because completion consumes
		// it - the declaration is reported for an upload a caller is
		// reconciling, not only for one it has already published.
		described, err := multi.GetMultipartUpload(ctx, upload.UploadID)
		if err != nil {
			t.Fatalf("get multipart upload: %v", err)
		}
		if described.Options.ChecksumAlgorithm != "CRC32" {
			t.Errorf("declared checksum algorithm = %q, want %q", described.Options.ChecksumAlgorithm, "CRC32")
		}

		part, err := multi.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("upload part: %v", err)
		}

		if _, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
			t.Fatalf("completing an upload that declared a checksum algorithm: %v", err)
		}

		reader, _, err := store.GetObject(ctx, "declared", "object.bin")
		if err != nil {
			t.Fatalf("get object: %v", err)
		}
		defer reader.Close()
		published, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read object: %v", err)
		}
		if string(published) != string(body) {
			t.Errorf("object body = %q, want %q", published, body)
		}
	})
}

// A checksum the client did supply is still verified against the assembled
// bytes, so the fix above is not "stop checking".
func TestStoreVerifiesASuppliedWholeObjectChecksumAtCompletion(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "supplied"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}

		body := []byte("hello")
		// A checksum of different bytes, so it disagrees with what the
		// completion assembles.
		wrong, err := storage.ComputeChecksum("CRC32", []byte("something else"))
		if err != nil {
			t.Fatalf("compute checksum: %v", err)
		}

		upload, err := multi.CreateMultipartUpload(ctx, "supplied", "object.bin", storage.MultipartOptions{
			ChecksumAlgorithm: "CRC32",
			ChecksumValue:     wrong,
		})
		if err != nil {
			t.Fatalf("create multipart upload: %v", err)
		}
		part, err := multi.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("upload part: %v", err)
		}
		if _, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err == nil {
			t.Fatal("a completion with a checksum that does not match the assembled bytes was accepted")
		}
	})
}
