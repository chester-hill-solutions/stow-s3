package workspace_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type ownerRecoveryFixture struct {
	store              *workspace.Store
	root, path         string
	upload             *storage.MultipartUpload
	part               storage.PartInfo
	original, retained []byte
}

func newOwnerRecoveryFixture(t *testing.T) *ownerRecoveryFixture {
	t.Helper()
	store, root := newStore(t)
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "zzz"); err != nil {
		t.Fatal(err)
	}
	upload, err := store.CreateMultipartUpload(ctx, "zzz", "target", storage.MultipartOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	part, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("replacement"))
	if err != nil {
		t.Fatal(err)
	}
	f := &ownerRecoveryFixture{store: store, root: root, upload: upload, part: *part}
	f.path = filepath.Join(workspace.InternalPath(root, "multipart", upload.UploadID), "upload.json")
	f.original, err = os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.retained, err = os.ReadFile(filepath.Join(filepath.Dir(f.path), "part-1"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *ownerRecoveryFixture) damage(t *testing.T, kind string) {
	t.Helper()
	if kind == "missing" || kind == "directory" {
		if err := os.Remove(f.path); err != nil {
			t.Fatal(err)
		}
		if kind == "directory" {
			if err := os.Mkdir(f.path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	data := []byte(map[string]string{"truncated": "{", "null": "null", "empty": "{}"}[kind])
	changes := map[string]struct{ field, value string }{
		"invalid-bucket": {"bucket", `"invalid/bucket"`},
		"invalid-key":    {"key", `""`},
		"wrong-id":       {"upload_id", `"other"`},
	}
	if change, ok := changes[kind]; ok {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(f.original, &fields); err != nil {
			t.Fatal(err)
		}
		fields[change.field] = json.RawMessage(change.value)
		var err error
		data, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *ownerRecoveryFixture) repair(t *testing.T, kind string) {
	t.Helper()
	if kind == "directory" {
		if err := os.Remove(f.path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.path, f.original, 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *ownerRecoveryFixture) assertRetained(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(f.path), "part-1"))
	if err != nil || !bytes.Equal(data, f.retained) {
		t.Fatalf("retained part changed: bytes=%d error=%v", len(data), err)
	}
}

func assertUnknownOwner(t *testing.T, operation string, err error) {
	t.Helper()
	if err == nil || errors.Is(err, storage.ErrUploadNotFound) || errors.Is(err, storage.ErrNoSuchUpload) {
		t.Errorf("%s hid damaged owner as success/absence: %v", operation, err)
	}
}

func TestWorkspaceMultipartOwnerFailuresPreserveParts(t *testing.T) {
	for _, kind := range []string{"truncated", "null", "empty", "invalid-bucket", "invalid-key", "wrong-id", "missing", "directory"} {
		t.Run(kind, func(t *testing.T) { checkWorkspaceOwnerFailure(t, kind) })
	}
}

func checkWorkspaceOwnerFailure(t *testing.T, kind string) {
	t.Helper()
	ctx := context.Background()
	f := newOwnerRecoveryFixture(t)
	f.damage(t, kind)
	_, err := f.store.GetMultipartUpload(ctx, f.upload.UploadID)
	assertUnknownOwner(t, "lookup", err)
	assertUnknownOwner(t, "validate", f.store.ValidateMultipartUpload(ctx, f.upload.UploadID, "zzz", "target"))
	_, err = f.store.ListParts(ctx, f.upload.UploadID)
	assertUnknownOwner(t, "parts", err)
	_, err = f.store.ListMultipartUploads(ctx, bucket, storage.MultipartListOptions{})
	assertUnknownOwner(t, "other-bucket listing", err)
	_, err = f.store.CompleteMultipartUpload(ctx, f.upload.UploadID, []storage.PartInfo{f.part})
	assertUnknownOwner(t, "completion", err)
	f.assertRetained(t)
	f.repair(t, kind)
	upload, err := f.store.GetMultipartUpload(ctx, f.upload.UploadID)
	if err != nil || upload.Options.ContentType != "text/plain" {
		t.Fatalf("repaired lookup=%+v error=%v", upload, err)
	}
	meta, err := f.store.CompleteMultipartUpload(ctx, f.upload.UploadID, []storage.PartInfo{f.part})
	if err != nil || meta.Size != 11 || meta.ContentType != "text/plain" {
		t.Fatalf("repaired completion=%+v error=%v", meta, err)
	}
}

func TestWorkspaceMultipartOwnerHealthyFiltering(t *testing.T) {
	f := newOwnerRecoveryFixture(t)
	for _, name := range []string{bucket, "zzz"} {
		listed, err := f.store.ListMultipartUploads(context.Background(), name, storage.MultipartListOptions{})
		want := 0
		if name == "zzz" {
			want = 1
		}
		if err != nil || len(listed.Uploads) != want {
			t.Fatalf("bucket=%s uploads=%+v error=%v", name, listed, err)
		}
	}
}

func TestWorkspaceMultipartOwnerAbsent(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	_, lookup := store.GetMultipartUpload(ctx, "absent")
	validation := store.ValidateMultipartUpload(ctx, "absent", bucket, "target")
	_, parts := store.ListParts(ctx, "absent")
	_, completion := store.CompleteMultipartUpload(ctx, "absent", []storage.PartInfo{{PartNumber: 1, ETag: "unused"}})
	for _, err := range []error{lookup, validation, parts, completion} {
		if !errors.Is(err, storage.ErrUploadNotFound) {
			t.Errorf("genuine absence changed: %v", err)
		}
	}
}

func reopenOwnerStore(t *testing.T, f *ownerRecoveryFixture) {
	t.Helper()
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = workspace.New(workspace.Options{Root: f.root, Bucket: bucket})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.store.Close() })
}

func openOwnerRuntime(f *ownerRecoveryFixture) (*stowruntime.Instance, error) {
	return stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendWorkspace, MaxBytes: 12, MaxObjects: 1}, f.store, nil)
}

func assertOwnerQuota(t *testing.T, instance *stowruntime.Instance, id string) {
	t.Helper()
	ctx := context.Background()
	if _, err := instance.PutObject(ctx, "zzz", "target", []byte("xx"), stowruntime.PutOptions{}); !errors.Is(err, stowruntime.ErrQuotaExceeded) {
		t.Fatalf("retained 11-byte part escaped 12-byte quota: %v", err)
	}
	if err := instance.AbortMultipartUpload(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, "zzz", "target", []byte("123456789012"), stowruntime.PutOptions{}); err != nil {
		t.Fatalf("abort did not release retained bytes: %v", err)
	}
}

func TestWorkspaceMultipartOwnerRecoveryRefusesUnknown(t *testing.T) {
	for _, kind := range []string{"truncated", "null", "wrong-id", "missing", "directory"} {
		t.Run(kind, func(t *testing.T) { checkOwnerReopen(t, kind) })
	}
}

func checkOwnerReopen(t *testing.T, kind string) {
	t.Helper()
	f := newOwnerRecoveryFixture(t)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.damage(t, kind)
	reopenOwnerStore(t, f)
	instance, err := openOwnerRuntime(f)
	if err == nil {
		_, putErr := instance.PutObject(context.Background(), "zzz", "target", []byte("123456789012"), stowruntime.PutOptions{})
		_ = instance.Close()
		t.Fatalf("reopened with unknown 11-byte part; new 12-byte write error=%v", putErr)
	}
	assertUnknownOwner(t, "runtime reopen", err)
	if instance != nil {
		t.Fatal("failed reopen returned a runtime")
	}
	f.assertRetained(t)
	f.repair(t, kind)
	instance, err = openOwnerRuntime(f)
	if err != nil {
		t.Fatalf("reopen after repair: %v", err)
	}
	defer instance.Close()
	assertOwnerQuota(t, instance, f.upload.UploadID)
}

func TestWorkspaceMultipartOwnerLiveRecoveryRetainsQuota(t *testing.T) {
	for _, kind := range []string{"truncated", "missing", "directory"} {
		t.Run(kind, func(t *testing.T) { checkOwnerLiveRecovery(t, kind) })
	}
}

func checkOwnerLiveRecovery(t *testing.T, kind string) {
	t.Helper()
	f := newOwnerRecoveryFixture(t)
	instance, err := openOwnerRuntime(f)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	ctx := context.Background()
	f.damage(t, kind)
	assertUnknownOwner(t, "live validation", instance.ValidateMultipartUpload(ctx, f.upload.UploadID, "zzz", "target"))
	if err := instance.RefreshUsage(ctx); err == nil {
		t.Error("refresh omitted unknown retained parts")
	}
	if _, err := instance.PutObject(ctx, "zzz", "target", []byte("xx"), stowruntime.PutOptions{}); err == nil {
		t.Error("write admitted after incomplete reconciliation")
	}
	f.assertRetained(t)
	f.repair(t, kind)
	if err := instance.RefreshUsage(ctx); err != nil {
		t.Fatal(err)
	}
	assertOwnerQuota(t, instance, f.upload.UploadID)
}

func TestWorkspaceMultipartOwnerFailedCreationCleansState(t *testing.T) {
	store, root := newStore(t)
	dir := workspace.InternalPath(root, "multipart")
	store.Now = func() time.Time {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("creation staging entries=%d error=%v", len(entries), err)
		}
		if err := os.Mkdir(filepath.Join(dir, entries[0].Name(), "upload.json"), 0700); err != nil {
			t.Fatal(err)
		}
		return time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	}
	defer func() { store.Now = nil }()
	_, err := store.CreateMultipartUpload(context.Background(), bucket, "target", storage.MultipartOptions{})
	store.Now = nil
	if err == nil {
		t.Fatal("obstructed owner publication succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed initiation stranded unknown uploads: entries=%d error=%v", len(entries), err)
	}
	if _, err := store.ListMultipartUploads(context.Background(), bucket, storage.MultipartListOptions{}); err != nil {
		t.Fatalf("failed initiation blocks future reconciliation: %v", err)
	}
}
