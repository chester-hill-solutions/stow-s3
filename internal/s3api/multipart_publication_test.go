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

	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func TestMultipartIndexFailureReturnsS3ErrorAfterPublication(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workspace.New(workspace.Options{Root: root, Bucket: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendWorkspace, MaxBytes: 8, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := runtime.NewStoreAdapter(instance)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, ".stow", "index.json")
	if err := os.Rename(index, index+".saved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(index); _ = os.Rename(index+".saved", index); _ = instance.Close() })
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>", part.ETag)
	request := httptest.NewRequest(http.MethodPost, "/probe/target?uploadId="+upload.UploadID, strings.NewReader(body))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "<Code>InternalError</Code>") {
		t.Fatalf("completion response: %d %s", response.Code, response.Body.String())
	}
	object, err := instance.GetObject(ctx, "probe", "target")
	if err != nil || string(object.Data) != "body" || object.ETag != storage.CompletionETag([]string{part.ETag}) {
		t.Fatalf("published object=%+v error=%v", object, err)
	}
	if _, err := instance.GetMultipartUpload(ctx, upload.UploadID); err != nil {
		t.Fatalf("failed completion discarded its upload: %v", err)
	}
}
