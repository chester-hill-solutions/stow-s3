package s3api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// A path the store cannot read has not answered the question. Reporting it as
// "no such key" tells a caller its object is gone, and a create-only write
// against that answer replaces bytes that are still there.
func TestAnUnreadableObjectIsNotReportedAsMissing(t *testing.T) {
	skipUnlessTheHostCanHideAnObject(t)
	server, root := workspaceServer(t)

	victim := filepath.Join(root, "victim")
	chmod(t, victim, 0o000)
	t.Cleanup(func() { _ = os.Chmod(victim, 0o600) })

	// A read that cannot be answered is a failure, not a missing key.
	read := httptest.NewRecorder()
	server.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/probe/victim", nil))
	if read.Code != http.StatusInternalServerError {
		t.Errorf("GET on an unreadable object = %d %s, want 500", read.Code, read.Body.String())
	}

	// A create-only write over it is refused rather than admitted.
	write := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/probe/victim", strings.NewReader("new"))
	request.Header.Set("If-None-Match", "*")
	// The handler requires the header itself, not only the request's own length
	// field, so a fixture that omits it is refused before it reaches the store.
	request.Header.Set("Content-Length", "3")
	server.ServeHTTP(write, request)
	if write.Code == http.StatusOK {
		t.Errorf("a create-only PUT over an unreadable object = %d %s, want a refusal", write.Code, write.Body.String())
	}
	if write.Code == http.StatusBadRequest {
		t.Errorf("a create-only PUT over an unreadable object = %d %s, want the store's refusal, not a malformed request", write.Code, write.Body.String())
	}

	// And the bytes are still there once the host can read them again.
	chmod(t, victim, 0o600)
	after, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read the object back: %v", err)
	}
	if string(after) != "old" {
		t.Errorf("the object now holds %q, want the original bytes %q", after, "old")
	}
}

// A root process can read a file with no permissions, so the case cannot be
// arranged there, and a host without a safe regular-file open cannot hold it
// at all.
func skipUnlessTheHostCanHideAnObject(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can read a file with no permissions, so this cannot be arranged here")
	}
	if !rooted.Supported() {
		t.Skip("this host has no safe regular-file open, so it cannot hold the case")
	}
	if !workspace.LockSupported() {
		t.Skip("this host has no advisory file lock, so it cannot hold the case")
	}
}

// A server over a workspace holding one object with known bytes.
func workspaceServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := workspace.New(workspace.Options{Root: root, Bucket: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "probe", "victim", bytes.NewReader([]byte("old")), storage.PutOptions{}); err != nil {
		t.Fatalf("write the object: %v", err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendWorkspace, MaxBytes: 1024, MaxObjects: 10}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	adapter, err := runtime.NewStoreAdapter(instance)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	return server, root
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}
