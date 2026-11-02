package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type filesystemCleanupFixture struct {
	instance *Instance
	store    *fs.FilesystemStore
	upload   string
	dir      string
	part     *storage.PartInfo
}

func newFilesystemCleanupFixture(t *testing.T) *filesystemCleanupFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := fs.NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	upload, err := store.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "retained"}})
	if err != nil {
		t.Fatal(err)
	}
	part, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := OpenWithStore(Options{Backend: BackendFilesystem, MaxBytes: 12, MaxObjects: 2, MaxMultipartUploads: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	return &filesystemCleanupFixture{instance: instance, store: store, upload: upload.UploadID, dir: filepath.Join(root, ".multipart", upload.UploadID), part: part}
}

func TestFilesystemCompletionCleanupOutcome(t *testing.T) {
	for _, fault := range []bool{false, true} {
		name := "healthy"
		if fault {
			name = "cleanup-failure"
		}
		t.Run(name, func(t *testing.T) { checkFilesystemCleanupOutcome(t, fault) })
	}
}

func checkFilesystemCleanupOutcome(t *testing.T, fault bool) {
	t.Helper()
	if fault && (goruntime.GOOS == "windows" || os.Geteuid() == 0) {
		t.Skip("cleanup fault requires enforced POSIX directory permissions")
	}
	f := newFilesystemCleanupFixture(t)
	if fault {
		if err := os.Chmod(f.dir, 0555); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(f.dir, 0755) }()
	}
	ctx := context.Background()
	meta, err := f.instance.CompleteMultipartUpload(ctx, f.upload, []storage.PartInfo{*f.part})
	assertFilesystemCleanupError(t, fault, err)
	if got := f.instance.Usage(); got != (Usage{Bytes: 4, Objects: 1}) {
		t.Errorf("immediate committed usage=%+v", got)
	}
	wantReserved := int64(0)
	if fault {
		wantReserved = 4
	}
	if f.instance.reservedBytes != wantReserved {
		t.Errorf("reserved part bytes=%d want=%d", f.instance.reservedBytes, wantReserved)
	}
	assertFilesystemCleanupObject(t, f, meta)
	if !fault {
		if _, err := f.store.GetMultipartUpload(ctx, f.upload); !errors.Is(err, storage.ErrUploadNotFound) {
			t.Errorf("healthy completion retained staging: %v", err)
		}
		return
	}
	assertFilesystemCleanupRecovery(t, f)
}

func assertFilesystemCleanupError(t *testing.T, fault bool, err error) {
	t.Helper()
	if !fault {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if !errors.Is(err, storage.ErrMutationCommitted) {
		t.Errorf("published completion lost committed marker: %v", err)
	}
	var pathErr *os.PathError
	if !errors.Is(err, os.ErrPermission) || !errors.As(err, &pathErr) {
		t.Errorf("original cleanup error lost: %v", err)
	}
}

func assertFilesystemCleanupObject(t *testing.T, f *filesystemCleanupFixture, meta *storage.ObjectMeta) {
	t.Helper()
	reader, stored, err := f.store.GetObject(context.Background(), "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "body" {
		t.Fatalf("published bytes=%q error=%v", data, err)
	}
	if meta == nil {
		t.Error("published completion lost metadata")
		return
	}
	if meta.ETag != storage.CompletionETag([]string{f.part.ETag}) || meta.VersionID == "" || meta.Size != 4 || meta.ContentType != "text/plain" || meta.Metadata["state"] != "retained" {
		t.Errorf("committed identity=%+v", meta)
	}
	if meta.VersionID != stored.VersionID || meta.ETag != stored.ETag || !meta.LastModified.Equal(stored.LastModified) {
		t.Errorf("returned identity differs from published record: returned=%+v stored=%+v", meta, stored)
	}
}

func assertFilesystemCleanupRecovery(t *testing.T, f *filesystemCleanupFixture) {
	t.Helper()
	ctx := context.Background()
	parts, err := f.store.ListParts(ctx, f.upload)
	if err != nil || len(parts) != 1 || parts[0].ETag != f.part.ETag {
		t.Fatalf("retained parts=%+v error=%v", parts, err)
	}
	if _, err := f.instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("retained staging released session slot: %v", err)
	}
	if err := os.Chmod(f.dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := f.instance.AbortMultipartUpload(ctx, f.upload); err != nil {
		t.Fatal(err)
	}
	if f.instance.reservedBytes != 0 || f.instance.Usage() != (Usage{Bytes: 4, Objects: 1}) {
		t.Errorf("abort accounting: usage=%+v reserved=%d", f.instance.Usage(), f.instance.reservedBytes)
	}
	if _, err := f.store.HeadObject(ctx, "probe", "target"); err != nil {
		t.Errorf("abort removed published object: %v", err)
	}
}
