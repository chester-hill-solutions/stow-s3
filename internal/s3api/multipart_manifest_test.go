package s3api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type multipartManifestWireFixture struct {
	server              *s3api.Server
	dir, uploadID, etag string
}

func newMultipartManifestWireFixture(t *testing.T) multipartManifestWireFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := fs.NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("old"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: store, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	return multipartManifestWireFixture{server: server, dir: filepath.Join(root, ".multipart", upload.UploadID), uploadID: upload.UploadID, etag: part.ETag}
}

func TestFilesystemMultipartManifestWireErrors(t *testing.T) {
	for _, state := range []string{"healthy", "absent", "truncated", "missing-manifest"} {
		t.Run(state, func(t *testing.T) { checkMultipartManifestWire(t, state) })
	}
}

func checkMultipartManifestWire(t *testing.T, state string) {
	t.Helper()
	f := newMultipartManifestWireFixture(t)
	listWant, completeWant := f.setState(t, state)
	list := httptest.NewRecorder()
	f.server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/bucket?uploads", nil))
	if list.Code != listWant {
		t.Errorf("listing status=%d want=%d body=%s", list.Code, listWant, list.Body)
	}
	completion := fmt.Sprintf("<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>", f.etag)
	response := httptest.NewRecorder()
	f.server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/bucket/target?uploadId="+f.uploadID, strings.NewReader(completion)))
	if response.Code != completeWant {
		t.Errorf("completion status=%d want=%d body=%s", response.Code, completeWant, response.Body)
	}
}

func (f multipartManifestWireFixture) setState(t *testing.T, state string) (int, int) {
	t.Helper()
	var err error
	listWant, completeWant := http.StatusOK, http.StatusOK
	switch state {
	case "absent":
		err = os.RemoveAll(f.dir)
		completeWant = http.StatusNotFound
	case "truncated":
		err = os.WriteFile(filepath.Join(f.dir, "manifest.json"), []byte("{"), 0600)
		listWant, completeWant = http.StatusInternalServerError, http.StatusInternalServerError
	case "missing-manifest":
		err = os.Remove(filepath.Join(f.dir, "manifest.json"))
		listWant, completeWant = http.StatusInternalServerError, http.StatusInternalServerError
	}
	if err != nil {
		t.Fatal(err)
	}
	return listWant, completeWant
}
