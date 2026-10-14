package fs

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestComputedChecksumSaveReplay(t *testing.T) {
	for _, replayOnly := range []bool{false, true} {
		name := "put"
		if replayOnly {
			name = "receipt"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dir := filepath.Join(t.TempDir(), "owned")
			store := saveStore(t, dir)
			initializeSaveBucket(t, store)
			payload := []byte("original payload")
			opts := storage.PutOptions{RequestKey: "checksum-save", ComputeChecksum: true, ChecksumAlgorithm: " sha256 ", Guard: &storage.WriteGuard{Absent: true}}
			original, err := store.PutObject(ctx, "saved", "object", bytes.NewReader(payload), opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.PutObject(ctx, "saved", "object", bytes.NewBufferString("replacement"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = saveStore(t, dir)
			var replayed *storage.ObjectMeta
			if replayOnly {
				var receipt storage.SaveReceipt
				var found bool
				receipt, found, err = store.ReplaySaveRequest(ctx, "saved", "object", payload, opts)
				if !found {
					t.Fatal("retained checksum save was not found")
				}
				replayed = receipt.Meta
			} else {
				replayed, err = store.PutObject(ctx, "saved", "object", bytes.NewReader(payload), opts)
			}
			if err != nil || replayed == nil || replayed.VersionID != original.VersionID || replayed.ChecksumValue != original.ChecksumValue {
				t.Fatalf("equivalent checksum retry must return the retained save: %+v, %v", replayed, err)
			}
			current, err := store.HeadObject(ctx, "saved", "object")
			if err != nil || current.ETag != storage.ETagForBytes([]byte("replacement")) {
				t.Fatalf("replay changed the current object: %+v, %v", current, err)
			}
			if _, err := store.PutObject(ctx, "saved", "object", bytes.NewBufferString("changed request"), opts); !errors.Is(err, storage.ErrSaveRequestConflict) {
				t.Fatalf("changed payload must conflict: %v", err)
			}
		})
	}
}
