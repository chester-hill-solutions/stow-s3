package stow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestWorkspaceIndexFailureReportsCommittedPublication(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, Bucket: "probe", RegistryDir: filepath.Join(t.TempDir(), "registry")})
	if err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, ".stow", "index.json")
	if err := os.Rename(index, index+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(index); err != nil {
			t.Error(err)
		}
		if err := os.Rename(index+".saved", index); err != nil {
			t.Error(err)
		}
		if err := ws.Close(); err != nil {
			t.Error(err)
		}
	})
	body := []byte("committed bytes")
	object, err := ws.PutObject(context.Background(), "probe", "report.txt", body, stow.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "new"}})
	actual, readErr := os.ReadFile(filepath.Join(root, "report.txt"))
	if readErr != nil || string(actual) != string(body) {
		t.Fatalf("published content=%q, error=%v", actual, readErr)
	}
	if !errors.Is(err, stow.ErrMutationCommitted) {
		t.Fatalf("published bytes reported as ordinary refusal: %v", err)
	}
	if object.ETag != storage.ETagForBytes(body) || object.Metadata["state"] != "new" {
		t.Fatalf("committed object identity missing: %+v", object)
	}
	if usage := ws.Usage(); usage.Bytes != int64(len(body)) || usage.Objects != 1 {
		t.Fatalf("committed publication omitted from usage: %+v", usage)
	}
}
