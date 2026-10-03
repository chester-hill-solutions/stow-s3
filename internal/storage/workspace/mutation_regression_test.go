package workspace_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceMutationsRefuseAncestorSymlink(t *testing.T) {
	for _, operation := range []string{"put", "delete"} {
		t.Run(operation, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			store := openStore(t, root, "bucket")
			defer store.Close()
			target := filepath.Join(outside, "secret")
			if err := os.WriteFile(target, []byte("private"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, "directory")); err != nil {
				t.Fatal(err)
			}
			var err error
			if operation == "put" {
				_, err = store.PutObject(context.Background(), "bucket", "directory/secret", strings.NewReader("overwrite"), storage.PutOptions{})
			} else {
				err = store.DeleteObject(context.Background(), "bucket", "directory/secret")
			}
			if err == nil {
				t.Error("accepted ancestor symlink mutation")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "private" {
				t.Fatalf("outside bytes changed: %q, %v", data, err)
			}
		})
	}
}

func TestCaseCollisionOverwriteDoesNotResurrect(t *testing.T) {
	root := t.TempDir()
	store := openStore(t, root, "bucket")
	ctx := context.Background()
	for _, item := range []struct{ key, data string }{{"Report.pdf", "A"}, {"report.pdf", "B"}} {
		if _, err := store.PutObject(ctx, "bucket", item.key, strings.NewReader(item.data), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteObject(ctx, "bucket", "Report.pdf"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "bucket", "report.pdf", strings.NewReader("C"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	body, _, err := store.GetObject(ctx, "bucket", "report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(data) != "C" {
		t.Fatalf("overwrite read %q, %v", data, err)
	}
	if err := store.DeleteObject(ctx, "bucket", "report.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, root, "bucket")
	defer store.Close()
	if body, _, err := store.GetObject(ctx, "bucket", "report.pdf"); !errors.Is(err, storage.ErrObjectNotFound) {
		if body != nil {
			body.Close()
		}
		t.Fatalf("deleted key resurrected: %v", err)
	}
}

func TestWorkspaceMutationRejectsAncestorChangedDuringBodyRead(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	store := openStore(t, root, "bucket")
	defer store.Close()
	parent := filepath.Join(root, "directory")
	if err := os.Mkdir(parent, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "secret")
	if err := os.WriteFile(target, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	reader := &replacingAncestorReader{t: t, parent: parent, outside: outside}
	if _, err := store.PutObject(context.Background(), "bucket", "directory/secret", reader, storage.PutOptions{}); err == nil {
		t.Fatal("accepted ancestor replaced during mutation")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "private" {
		t.Fatalf("outside changed: %q %v", data, err)
	}
}

type replacingAncestorReader struct {
	t               *testing.T
	parent, outside string
	done            bool
}

func (r *replacingAncestorReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	if err := os.Remove(r.parent); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Symlink(r.outside, r.parent); err != nil {
		r.t.Fatal(err)
	}
	return copy(p, "changed"), nil
}
