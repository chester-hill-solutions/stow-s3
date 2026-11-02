package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	disk "github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type multipartRecoveryFixture struct {
	root, manifest string
	original       []byte
	store          *disk.FilesystemStore
	upload         *storage.MultipartUpload
}

func openMultipartRecoveryDisk(t *testing.T, root string) *disk.FilesystemStore {
	t.Helper()
	store, err := disk.NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newMultipartRecoveryFixture(t *testing.T) *multipartRecoveryFixture {
	t.Helper()
	f := &multipartRecoveryFixture{root: t.TempDir()}
	f.store = openMultipartRecoveryDisk(t, f.root)
	ctx := context.Background()
	// Recovery scans aaa first; a damaged upload's bucket cannot be inferred.
	for _, bucket := range []string{"aaa", "zzz"} {
		if err := f.store.CreateBucket(ctx, bucket); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	f.upload, err = f.store.CreateMultipartUpload(ctx, "zzz", "target", storage.MultipartOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UploadPart(ctx, f.upload.UploadID, 1, strings.NewReader("old")); err != nil {
		t.Fatal(err)
	}
	f.manifest = filepath.Join(f.root, ".multipart", f.upload.UploadID, "manifest.json")
	f.original, err = os.ReadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *multipartRecoveryFixture) damage(t *testing.T, damage string) {
	t.Helper()
	var err error
	switch damage {
	case "truncated":
		err = os.WriteFile(f.manifest, []byte("{"), 0600)
	case "missing":
		err = os.Remove(f.manifest)
	case "directory":
		if err = os.Remove(f.manifest); err == nil {
			err = os.Mkdir(f.manifest, 0700)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
}

func (f *multipartRecoveryFixture) repair(t *testing.T, damage string) {
	t.Helper()
	if damage == "directory" {
		if err := os.Remove(f.manifest); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.manifest, f.original, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestFilesystemMultipartRecoveryRefusesDamagedManifest(t *testing.T) {
	for _, damage := range []string{"truncated", "missing", "directory"} {
		t.Run(damage, func(t *testing.T) { checkFilesystemMultipartRecovery(t, damage) })
	}
}

func checkFilesystemMultipartRecovery(t *testing.T, damage string) {
	t.Helper()
	ctx := context.Background()
	f := newMultipartRecoveryFixture(t)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.damage(t, damage)
	f.store = openMultipartRecoveryDisk(t, f.root)
	options := Options{Backend: BackendFilesystem, MaxBytes: 4, MaxObjects: 1, MaxMultipartUploads: 1}
	instance, openErr := OpenWithStore(options, f.store, nil)
	if openErr == nil {
		_, putErr := instance.PutObject(ctx, "zzz", "target", []byte("full"), PutOptions{})
		_ = instance.Close()
		t.Fatalf("opened with damaged upload; new 4-byte write error=%v while 3-byte part remains", putErr)
	}
	if instance != nil || errors.Is(openErr, storage.ErrUploadNotFound) || errors.Is(openErr, storage.ErrNoSuchUpload) {
		t.Fatalf("unknown state treated as absence: instance=%v error=%v", instance, openErr)
	}
	part, err := os.ReadFile(filepath.Join(filepath.Dir(f.manifest), "part-00001"))
	if err != nil || string(part) != "old" {
		t.Fatalf("failed open changed part: %q %v", part, err)
	}
	f.repair(t, damage)
	instance, err = OpenWithStore(options, f.store, nil)
	if err != nil {
		t.Fatalf("open after repair: %v", err)
	}
	defer instance.Close()
	assertRecoveryReservation(t, instance)
	assertRepairedMultipartAdmission(t, instance, f.upload.UploadID)
}

func assertRecoveryReservation(t *testing.T, instance *Instance) {
	t.Helper()
	if instance.reservedBytes != 3 || len(instance.multipart) != 1 {
		t.Errorf("retained reservation: bytes=%d uploads=%d", instance.reservedBytes, len(instance.multipart))
	}
}

func assertRepairedMultipartAdmission(t *testing.T, instance *Instance, upload string) {
	t.Helper()
	ctx := context.Background()
	if _, err := instance.PutObject(ctx, "zzz", "target", []byte("full"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("repaired part escaped quota: %v", err)
	}
	if err := instance.AbortMultipartUpload(ctx, upload); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "zzz", "target", []byte("full"), PutOptions{}); err != nil {
		t.Fatalf("abort did not release quota: %v", err)
	}
}

func TestFilesystemMultipartValidationPreservesUnknownReservation(t *testing.T) {
	ctx := context.Background()
	f := newMultipartRecoveryFixture(t)
	instance, err := OpenWithStore(Options{Backend: BackendFilesystem, MaxBytes: 4, MaxObjects: 1}, f.store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	f.damage(t, "truncated")
	if err := instance.ValidateMultipartUpload(ctx, f.upload.UploadID, "zzz", "target"); err == nil || errors.Is(err, storage.ErrNoSuchUpload) || errors.Is(err, storage.ErrUploadNotFound) {
		t.Errorf("validation misclassified uncertainty: %v", err)
	}
	assertRecoveryReservation(t, instance)
	if err := instance.RefreshUsage(ctx); err == nil {
		t.Error("refresh accepted unreadable multipart state")
	}
	assertRecoveryReservation(t, instance)
	if _, err := instance.PutObject(ctx, "zzz", "target", []byte("full"), PutOptions{}); err == nil {
		t.Error("mutation admitted after failed reconciliation")
	}
	f.repair(t, "truncated")
	if err := instance.RefreshUsage(ctx); err != nil {
		t.Fatal(err)
	}
	assertRecoveryReservation(t, instance)
}
