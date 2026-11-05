package storage_test

// An upload ID is caller-supplied at every store method that takes one, and the
// path-backed stores join it into a path, so the shape check is a traversal
// defence. The case lives here rather than in the shared backend contract file
// because that file is at the size ratchet's ceiling.

import (
	"context"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// An upload ID is caller-supplied at every store method that takes one, and the
// path-backed stores join it into a path. filepath.Join cleans "..", so an ID that
// is not the shape NewUploadID produces can address a directory outside the one
// uploads live in.
//
// The suite asserts refusal rather than one sentinel, because the backends differ
// on purpose here: the memory backend keeps uploads in a map and has no path to
// escape, so it answers from its own lookup, while the path-backed stores refuse
// the shape before a path is built.
func TestStoreRefusesAnUploadIDItDidNotMint(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		multi := requireMultipart(t, store)
		ctx := context.Background()
		if err := store.CreateBucket(ctx, "contract"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		minted, err := multi.CreateMultipartUpload(ctx, "contract", "key", storage.MultipartOptions{})
		if err != nil {
			t.Fatalf("create upload: %v", err)
		}

		ids := []struct {
			label string
			id    string
		}{
			{"traversal", "../../escaped"},
			{"parent", ".."},
			{"dot", "."},
			{"empty", ""},
			{"absolute", "/etc"},
			{"separator", "nested/id"},
			{"too short", strings.Repeat("a", 31)},
			{"not hexadecimal", strings.Repeat("g", 32)},
			{"uppercase hexadecimal", strings.Repeat("A", 32)},
			{"well shaped but never minted", strings.Repeat("a", 32)},
		}
		for _, testCase := range ids {
			t.Run(testCase.label, func(t *testing.T) {
				id := testCase.id
				if _, err := multi.GetMultipartUpload(ctx, id); err == nil {
					t.Error("GetMultipartUpload accepted an ID the store did not mint")
				}
				if _, err := multi.UploadPart(ctx, id, 1, strings.NewReader("part")); err == nil {
					t.Error("UploadPart accepted an ID the store did not mint")
				}
				if _, err := multi.CompleteMultipartUpload(ctx, id, []storage.PartInfo{{PartNumber: 1, ETag: `"x"`}}); err == nil {
					t.Error("CompleteMultipartUpload accepted an ID the store did not mint")
				}
				if err := multi.AbortMultipartUpload(ctx, id); err == nil {
					t.Error("AbortMultipartUpload accepted an ID the store did not mint")
				}
				if _, err := multi.ListParts(ctx, id); err == nil {
					t.Error("ListParts accepted an ID the store did not mint")
				}
				if err := multi.ValidateMultipartUpload(ctx, id, "contract", "key"); err == nil {
					t.Error("ValidateMultipartUpload accepted an ID the store did not mint")
				}
			})
		}

		// The guard is on shape, not identity: the ID this store minted still works,
		// so a real upload is not caught by it.
		if _, err := multi.GetMultipartUpload(ctx, minted.UploadID); err != nil {
			t.Fatalf("GetMultipartUpload refused a minted ID: %v", err)
		}
		if parts, err := multi.ListParts(ctx, minted.UploadID); err != nil || len(parts) != 0 {
			t.Fatalf("ListParts on a minted upload: parts=%v err=%v, want no parts and no error", parts, err)
		}
	})
}
