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
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestWorkspaceMultipartOwnerWire(t *testing.T) {
	for _, state := range []string{"healthy", "absent", "truncated", "missing"} {
		t.Run(state, func(t *testing.T) { checkWorkspaceOwnerWire(t, state) })
	}
}

func checkWorkspaceOwnerWire(t *testing.T, state string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := workspace.New(workspace.Options{Root: root, Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part, err := store.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("replacement"))
	if err != nil {
		t.Fatal(err)
	}
	dir := workspace.InternalPath(root, "multipart", upload.UploadID)
	listWant, completeWant := setWorkspaceOwnerWireState(t, dir, state)
	server, err := s3api.New(s3api.Config{Store: store, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/bucket?uploads", nil))
	if list.Code != listWant {
		t.Errorf("listing=%d want=%d body=%s", list.Code, listWant, list.Body)
	}
	body := fmt.Sprintf("<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>", part.ETag)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/bucket/target?uploadId="+upload.UploadID, strings.NewReader(body)))
	if response.Code != completeWant {
		t.Errorf("completion=%d want=%d body=%s", response.Code, completeWant, response.Body)
	}
}

func setWorkspaceOwnerWireState(t *testing.T, dir, state string) (int, int) {
	t.Helper()
	list, complete := http.StatusOK, http.StatusOK
	var err error
	switch state {
	case "absent":
		err = os.RemoveAll(dir)
		complete = http.StatusNotFound
	case "truncated":
		err = os.WriteFile(filepath.Join(dir, "upload.json"), []byte("{"), 0600)
		list, complete = http.StatusInternalServerError, http.StatusInternalServerError
	case "missing":
		err = os.Remove(filepath.Join(dir, "upload.json"))
		list, complete = http.StatusInternalServerError, http.StatusInternalServerError
	}
	if err != nil {
		t.Fatal(err)
	}
	return list, complete
}
