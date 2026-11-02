package fs_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	fsstore "github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

func verifyCompletionBuffer(t *testing.T, path string, ns *capacity.Namespace, meta *storage.ObjectMeta, digest string) {
	t.Helper()
	var reopened *fsstore.FilesystemStore
	var err error
	if ns == nil {
		reopened, err = fsstore.NewFilesystemStore(path)
	} else {
		reopened, err = fsstore.NewFilesystemStoreWithNamespace(path, ns)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	body, stored, err := reopened.GetObject(context.Background(), "bucket", "payload")
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	n, err := io.Copy(h, body)
	closeErr := body.Close()
	if err != nil || closeErr != nil || n != meta.Size || hex.EncodeToString(h.Sum(nil)) != digest || stored.VersionID != meta.VersionID || stored.ETag != meta.ETag {
		t.Fatalf("reopened content/meta changed: n=%d meta=%+v err=%v close=%v", n, stored, err, closeErr)
	}
	if ns != nil {
		v, e := ns.Snapshot(context.Background())
		if e != nil || v.PayloadBytes < meta.Size || v.PayloadBytes > meta.Size+4096 || v.PendingBytes != 0 {
			t.Fatalf("completion charge %+v %v", v, e)
		}
	}
}
