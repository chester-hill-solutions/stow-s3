package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type completionHTTPStore struct {
	*workspace.Store
	completed bool
	lookupErr error
}

func (s *completionHTTPStore) CompleteMultipartUpload(ctx context.Context, id string, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	meta, err := s.Store.CompleteMultipartUpload(ctx, id, parts)
	s.completed = true
	return meta, err
}

func (s *completionHTTPStore) GetMultipartUpload(ctx context.Context, id string) (*storage.MultipartUpload, error) {
	if s.completed {
		return nil, s.lookupErr
	}
	return s.Store.GetMultipartUpload(ctx, id)
}

func TestCompletionLookupS3Recovery(t *testing.T) {
	for _, order := range []string{"marker-first", "missing-first"} {
		t.Run(order, func(t *testing.T) { checkCompletionLookupHTTP(t, order) })
	}
}

func checkCompletionLookupHTTP(t *testing.T, order string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	backing, err := workspace.New(workspace.Options{Root: root, Bucket: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("upload observation unavailable")
	lookupErr := errors.Join(storage.ErrMutationCommitted, storage.ErrUploadNotFound, unavailable)
	if order == "missing-first" {
		lookupErr = errors.Join(storage.ErrUploadNotFound, unavailable, storage.ErrMutationCommitted)
	}
	store := &completionHTTPStore{Store: backing, lookupErr: lookupErr}
	var set policy.Set
	set.Add("runtime", "probe", policy.Selector{Kind: policy.KindObject, Prefix: "target"}, policy.Allow, authority.ObjectRead, authority.ObjectWrite)
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendWorkspace, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1, Policy: policy.Fixed(&set)}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	upload, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	repair := blockCompletionHTTPAbort(t, root, upload.UploadID)
	adapter, err := stowruntime.NewStoreAdapter(instance)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>", part.ETag)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/probe/target?uploadId="+upload.UploadID, strings.NewReader(body)))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "<Code>InternalError</Code>") {
		t.Fatalf("completion status=%d body=%s", response.Code, response.Body.String())
	}
	before, err := instance.GetObject(ctx, "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	repair()
	store.completed = false
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/probe/target?uploadId="+upload.UploadID, nil))
	if response.Code != http.StatusNoContent {
		t.Errorf("policy-scoped abort status=%d body=%s", response.Code, response.Body.String())
	}
	assertHTTPCompletionObject(t, instance, before)
	if _, err := instance.PutObject(ctx, "probe", "target", []byte("12345678"), stowruntime.PutOptions{}); err != nil {
		t.Errorf("part capacity not reusable: %v", err)
	}
}

func blockCompletionHTTPAbort(t *testing.T, root, id string) func() {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("cleanup fault requires enforced permissions")
	}
	dir := filepath.Join(root, ".stow", "multipart", id)
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	repair := func() { _ = os.Chmod(dir, 0755) }
	t.Cleanup(repair)
	probe := filepath.Join(dir, "permission-probe")
	if err := os.WriteFile(probe, nil, 0600); err == nil {
		_ = os.Remove(probe)
		t.Skip("host does not enforce directory permissions")
	}
	return repair
}

func assertHTTPCompletionObject(t *testing.T, instance *stowruntime.Instance, before stowruntime.Object) {
	t.Helper()
	after, err := instance.GetObject(context.Background(), "probe", "target")
	if err != nil || string(after.Data) != "body" || after.VersionID != before.VersionID || after.ETag != before.ETag {
		t.Fatalf("abort changed committed object: %+v error=%v", after, err)
	}
}
