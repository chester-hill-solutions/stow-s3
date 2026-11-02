package fs

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// Admission must describe the complete physical record, including its header.
func TestMultipartCompletionCapacityVersion(t *testing.T) {
	for _, data := range []string{"", "verified payload"} {
		t.Run(data, func(t *testing.T) {
			ns := publicationNamespace(t)
			store := publicationStore(t, ns, t.TempDir())
			ctx := context.Background()
			sum, err := storage.ComputeChecksum("SHA256", []byte(data))
			if err != nil {
				t.Fatal(err)
			}
			u, err := store.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{ChecksumAlgorithm: "SHA256", ChecksumValue: sum})
			if err != nil {
				t.Fatal(err)
			}
			part, err := store.UploadPart(ctx, u.UploadID, 1, strings.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			meta, err := store.CompleteMultipartUpload(ctx, u.UploadID, []storage.PartInfo{*part})
			if err != nil {
				t.Fatal(err)
			}
			if meta.ChecksumValue != sum || meta.ChecksumAlgorithm != "SHA256" || meta.Size != int64(len(data)) {
				t.Fatalf("checksum/size lost: %+v", meta)
			}
			path := store.objectPath("bucket", "key")
			physical, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ref := resourceFor(path, physical, capacity.Payload)
			ref.StoreID = store.saves.id
			// The physical record must be usable as a capacity dependency.
			err = ns.BeginOperation(ctx, capacity.Operation{ID: "pin-completion", Owner: "test", Meaning: "verify-record", Dependencies: []capacity.Resource{ref}})
			if err != nil {
				t.Fatalf("admitted resource differs from published bytes: %v", err)
			}
			if err := ns.ReleaseOperation(ctx, "pin-completion", "test"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
