package workspace_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type partPublicationReader struct {
	io.Reader
	before func()
}

func (r *partPublicationReader) Read(data []byte) (int, error) {
	if r.before != nil {
		f := r.before
		r.before = nil
		f()
	}
	return r.Reader.Read(data)
}

func blockPartUploadDocument(t *testing.T, file string) func() {
	t.Helper()
	saved := file + ".saved"
	if err := os.Rename(file, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		_ = os.Rename(saved, file)
		t.Fatal(err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(saved, file); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func replacePublishedPart(t *testing.T, old, replacement string, fault bool) (*workspace.Store, string) {
	t.Helper()
	ctx := context.Background()
	store, root := newStore(t)
	upload, err := store.CreateMultipartUpload(ctx, bucket, "target", storage.MultipartOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	if old != "" {
		if _, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader(old)); err != nil {
			t.Fatal(err)
		}
	}
	restore := func() {}
	defer func() { restore() }()
	body := &partPublicationReader{Reader: strings.NewReader(replacement)}
	if fault {
		body.before = func() {
			restore = blockPartUploadDocument(t, filepath.Join(workspace.InternalPath(root, "multipart", upload.UploadID), "upload.json"))
		}
	}
	_, writeErr := store.UploadPart(ctx, upload.UploadID, 1, body)
	if !fault && writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Logf("replacement result error=%v", writeErr)
	restore()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workspace.New(workspace.Options{Root: root, Bucket: bucket})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened, upload.UploadID
}

func TestMultipartPartDescriptorSurvivesMetadataFailure(t *testing.T) {
	for _, pair := range []struct{ name, old, replacement string }{{"first", "", "replacement"}, {"larger", "old", "replacement"}, {"same-size", "old", "new"}} {
		for _, fault := range []bool{false, true} {
			name := pair.name + "/healthy"
			if fault {
				name = pair.name + "/metadata-failure"
			}
			t.Run(name, func(t *testing.T) { checkPublishedPartDescriptor(t, pair.old, pair.replacement, fault) })
		}
	}
}

func checkPublishedPartDescriptor(t *testing.T, old, replacement string, fault bool) {
	t.Helper()
	store, id := replacePublishedPart(t, old, replacement, fault)
	ctx := context.Background()
	parts, err := store.ListParts(ctx, id)
	if err != nil || len(parts) != 1 {
		t.Fatalf("reopened parts=%+v error=%v", parts, err)
	}
	if parts[0].Size != int64(len(replacement)) || !storage.ETagEqual(parts[0].ETag, storage.ETagForBytes([]byte(replacement))) {
		t.Errorf("listed descriptor does not identify published bytes: %+v", parts[0])
	}
	meta, err := store.CompleteMultipartUpload(ctx, id, parts)
	if err != nil {
		t.Fatalf("completion from reopened list: %v", err)
	}
	if meta.Size != int64(len(replacement)) || meta.ContentType != "text/plain" {
		t.Errorf("completed metadata=%+v", meta)
	}
	if got := get(t, store, "target"); got != replacement {
		t.Errorf("completed bytes=%q want=%q", got, replacement)
	}
}

func TestMultipartPartRecoveryPreservesQuota(t *testing.T) {
	store, id := replacePublishedPart(t, "old", "replacement", true)
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendWorkspace, MaxBytes: 12, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	ctx := context.Background()
	if _, err := instance.PutObject(ctx, bucket, "target", []byte("xx"), stowruntime.PutOptions{}); !errors.Is(err, stowruntime.ErrQuotaExceeded) {
		t.Errorf("11-byte part escaped 12-byte quota: %v", err)
	}
	if _, err := instance.PutObject(ctx, bucket, "target", []byte("x"), stowruntime.PutOptions{}); err != nil {
		t.Fatalf("exact-capacity write: %v", err)
	}
	if err := instance.AbortMultipartUpload(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.PutObject(ctx, bucket, "target", []byte("123456789012"), stowruntime.PutOptions{}); err != nil {
		t.Fatalf("abort did not release retained part: %v", err)
	}
}
