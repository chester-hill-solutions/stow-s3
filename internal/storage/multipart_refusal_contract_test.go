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
