package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	disk "github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// Seed real manifests and parts at the escaped path. A missing-path error is not
// evidence that an upload ID was refused before it reached the filesystem.
const escapedID = "../../escaped"

type escapedUploadFixture struct {
	store   storage.Store
	multi   storage.MultipartStore
	escaped string
	part    storage.PartInfo
}

func TestMultipartTraversalCannotAccessExistingStagedBytes(t *testing.T) {
	for _, backend := range []string{"filesystem", "workspace"} {
		for _, operation := range []string{"get", "upload", "complete", "abort", "list", "page", "validate"} {
			t.Run(backend+"/"+operation, func(t *testing.T) {
				fixture := seedEscapedUpload(t, backend)
				before := stagedTree(t, fixture.escaped)
				body := &countingReader{Reader: strings.NewReader("replacement bytes")}
				err := callEscapedUpload(fixture.multi, operation, fixture.part, body)
				want := storage.ErrUploadNotFound
				if backend == "filesystem" && operation == "validate" {
					want = storage.ErrNoSuchUpload
				}
				if !errors.Is(err, want) {
					t.Errorf("%s = %v, want %v", operation, err, want)
				}
				if body.reads != 0 {
					t.Errorf("refused upload read its body %d times", body.reads)
				}
				if after := stagedTree(t, fixture.escaped); !reflect.DeepEqual(after, before) {
					t.Errorf("escaped staged files changed: before=%v after=%v", before, after)
				}
				assertEscapedObjectAbsent(t, fixture.store)
				assertMintedUploadWorks(t, fixture.multi)
			})
		}
	}
}

func seedEscapedUpload(t *testing.T, backend string) escapedUploadFixture {
	t.Helper()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "store")
	var store storage.Store
	var err error
	uploadRoot := filepath.Join(root, ".multipart")
	if backend == "filesystem" {
		store, err = disk.NewFilesystemStore(root)
	} else {
		store, err = workspace.New(workspace.Options{Root: root, Bucket: "contract"})
		uploadRoot = workspace.InternalPath(root, "multipart")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.CreateBucket(ctx, "contract"); err != nil {
		t.Fatal(err)
	}
	multi := requireMultipart(t, store)
	upload, err := multi.CreateMultipartUpload(ctx, "contract", "sentinel-key", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part, err := multi.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("outside sentinel bytes"))
	if err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(uploadRoot, escapedID)
	if err := os.Rename(filepath.Join(uploadRoot, upload.UploadID), escaped); err != nil {
		t.Fatal(err)
	}
	if backend == "workspace" {
		rewriteEscapedUpload(t, escaped)
	}
	return escapedUploadFixture{store: store, multi: multi, escaped: escaped, part: *part}
}

func rewriteEscapedUpload(t *testing.T, escaped string) {
	t.Helper()
	// The workspace record also authenticates its own ID. Make the fixture
	// internally consistent so that check cannot mask missing path validation.
	path := filepath.Join(escaped, "upload.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state["upload_id"], err = json.Marshal(escapedID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func callEscapedUpload(multi storage.MultipartStore, operation string, part storage.PartInfo, body *countingReader) error {
	ctx := context.Background()
	var err error
	switch operation {
	case "get":
		_, err = multi.GetMultipartUpload(ctx, escapedID)
	case "upload":
		_, err = multi.UploadPart(ctx, escapedID, 1, body)
	case "complete":
		_, err = multi.CompleteMultipartUpload(ctx, escapedID, []storage.PartInfo{part})
	case "abort":
		err = multi.AbortMultipartUpload(ctx, escapedID)
	case "list":
		_, err = multi.ListParts(ctx, escapedID)
	case "page":
		lister := multi.(interface {
			ListPartsPage(context.Context, string, storage.ListPartsOptions) (*storage.ListPartsResult, error)
		})
		_, err = lister.ListPartsPage(ctx, escapedID, storage.ListPartsOptions{MaxParts: 1})
	case "validate":
		err = multi.ValidateMultipartUpload(ctx, escapedID, "contract", "sentinel-key")
	}
	return err
}

func assertEscapedObjectAbsent(t *testing.T, store storage.Store) {
	t.Helper()
	object, _, err := store.GetObject(context.Background(), "contract", "sentinel-key")
	if object != nil {
		object.Close()
	}
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Errorf("escaped completion published an object: %v", err)
	}
}

func assertMintedUploadWorks(t *testing.T, multi storage.MultipartStore) {
	t.Helper()
	ctx := context.Background()
	valid, err := multi.CreateMultipartUpload(ctx, "contract", "valid-key", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := multi.UploadPart(ctx, valid.UploadID, 1, strings.NewReader("valid")); err != nil {
		t.Fatalf("minted upload refused: %v", err)
	}
}

func stagedTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative] = string(raw)
		return nil
	})
	if err != nil {
		t.Errorf("escaped fixture no longer readable: %v", err)
	}
	return files
}
