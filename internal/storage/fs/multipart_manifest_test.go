package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestMultipartManifestFailuresPreserveUpload(t *testing.T) {
	for _, damage := range []string{"truncated", "null", "empty", "invalid-bucket", "invalid-key", "missing", "directory"} {
		t.Run(damage, func(t *testing.T) {
			checkMultipartManifestFailure(t, damage)
		})
	}
}

func checkMultipartManifestFailure(t *testing.T, damage string) {
	t.Helper()
	ctx := context.Background()
	store := recordStore(t)
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "target", storage.MultipartOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	part, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("old"))
	if err != nil {
		t.Fatal(err)
	}
	dir := store.multipartDir(upload.UploadID)
	manifest := filepath.Join(dir, "manifest.json")
	original, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	damageMultipartManifest(t, manifest, damage)
	assertDamagedMultipartManifest(t, store, upload.UploadID, *part)
	data, err := os.ReadFile(filepath.Join(dir, "part-00001"))
	if err != nil || string(data) != "old" {
		t.Fatalf("failed operations changed retained part: %q %v", data, err)
	}
	if _, err := store.HeadObject(ctx, "bucket", "target"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("damaged upload published an object: %v", err)
	}
	if damage == "directory" {
		if err := os.Remove(manifest); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(manifest, original, 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.GetMultipartUpload(ctx, upload.UploadID)
	if err != nil || recovered.Key != "target" || recovered.Options.ContentType != "text/plain" {
		t.Fatalf("repaired lookup=%+v %v", recovered, err)
	}
	if _, err := store.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
		t.Fatalf("repaired completion: %v", err)
	}
}

func TestMultipartAbsentUploadKeepsNotFound(t *testing.T) {
	ctx := context.Background()
	store := recordStore(t)
	if _, err := store.GetMultipartUpload(ctx, "absent"); !errors.Is(err, storage.ErrUploadNotFound) {
		t.Errorf("lookup=%v", err)
	}
	if err := store.ValidateMultipartUpload(ctx, "absent", "bucket", "target"); !errors.Is(err, storage.ErrNoSuchUpload) {
		t.Errorf("validation=%v", err)
	}
	if _, err := store.CompleteMultipartUpload(ctx, "absent", []storage.PartInfo{{PartNumber: 1, ETag: "unused"}}); !errors.Is(err, storage.ErrUploadNotFound) {
		t.Errorf("completion=%v", err)
	}
	listed, err := store.ListMultipartUploads(ctx, "bucket", storage.MultipartListOptions{})
	if err != nil || len(listed.Uploads) != 0 {
		t.Fatalf("listing=%+v %v", listed, err)
	}
}

func damageMultipartManifest(t *testing.T, manifest, damage string) {
	t.Helper()
	var err error
	switch damage {
	case "missing":
		err = os.Remove(manifest)
	case "directory":
		if err = os.Remove(manifest); err == nil {
			err = os.Mkdir(manifest, 0700)
		}
	default:
		body := map[string]string{"truncated": "{", "null": "null", "empty": "{}", "invalid-bucket": `{"bucket":"invalid/bucket","key":"target"}`, "invalid-key": `{"bucket":"bucket","key":""}`}[damage]
		err = os.WriteFile(manifest, []byte(body), 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func assertDamagedMultipartManifest(t *testing.T, store *FilesystemStore, uploadID string, part storage.PartInfo) {
	t.Helper()
	ctx := context.Background()
	_, lookupErr := store.GetMultipartUpload(ctx, uploadID)
	_, listErr := store.ListMultipartUploads(ctx, "bucket", storage.MultipartListOptions{})
	validateErr := store.ValidateMultipartUpload(ctx, uploadID, "bucket", "target")
	_, completeErr := store.CompleteMultipartUpload(ctx, uploadID, []storage.PartInfo{part})
	for _, outcome := range []struct {
		name string
		err  error
	}{{"lookup", lookupErr}, {"list", listErr}, {"validate", validateErr}, {"complete", completeErr}} {
		if outcome.err == nil || errors.Is(outcome.err, storage.ErrUploadNotFound) || errors.Is(outcome.err, storage.ErrNoSuchUpload) {
			t.Errorf("%s hid damaged upload as success/absence: %v", outcome.name, outcome.err)
		}
	}
}
