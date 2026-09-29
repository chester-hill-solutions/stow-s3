package stow

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func portableFixture(t *testing.T) (*Workspace, string) {
	t.Helper()
	registry := filepath.Join(t.TempDir(), "registry")
	w, err := OpenWorkspace(WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "source"), RegistryDir: registry, Bucket: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	ctx := context.Background()
	for _, bucket := range []string{"secondary", "empty-bucket"} {
		if err := w.CreateBucket(ctx, bucket); err != nil {
			t.Fatal(err)
		}
	}
	for _, object := range []struct{ bucket, key, body string }{{"primary", "normal.txt", "hello world"}, {"primary", "odd?/key", "escaped"}, {"secondary", "normal.txt", "secondary"}, {"secondary", "odd?/key", "other escaped"}, {"secondary", "../.env", "secret"}} {
		_, err := w.store.PutObject(ctx, object.bucket, object.key, strings.NewReader(object.body), storage.PutOptions{ContentType: "application/custom", Metadata: map[string]string{"X-Amz-Meta-Project": "stow"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(w.Dir(), "normal.txt"), 0o660); err != nil {
		t.Fatal(err)
	}
	return w, registry
}

func TestPortableCheckpointRoundTripObjectsAndEmptyBuckets(t *testing.T) {
	w, registry := portableFixture(t)
	ctx := context.Background()
	checkpoint, err := w.CreateCheckpoint(ctx, CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadCheckpoint(registry, checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 2 || len(manifest.Objects) != 4 || len(manifest.Payloads) != 3 || len(manifest.Buckets) != 3 {
		t.Fatalf("inventory = %+v", manifest)
	}
	if len(manifest.Excluded) != 1 || !strings.Contains(manifest.Excluded[0], ".env") {
		t.Fatalf("exclusions = %v", manifest.Excluded)
	}
	var archive bytes.Buffer
	if err := ExportCheckpoint(ctx, registry, checkpoint.ID, &archive, CheckpointArchiveOptions{}); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewCheckpointArchive(ctx, bytes.NewReader(archive.Bytes()), CheckpointArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Objects) != 4 || preview.Bytes != checkpoint.Bytes {
		t.Fatalf("preview = %+v", preview)
	}
	remote := filepath.Join(t.TempDir(), "registry")
	imported, err := ImportCheckpoint(ctx, remote, bytes.NewReader(archive.Bytes()), CheckpointArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreCheckpoint(remote, imported.ID, WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "restore"), RegistryDir: remote})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertPortableRestoredObjects(t, restored, manifest)
	assertPortableRestoredLineage(t, restored, remote, checkpoint.ID)
}

func assertPortableRestoredObjects(t *testing.T, restored *Workspace, manifest CheckpointManifest) {
	t.Helper()
	ctx := context.Background()
	for _, object := range manifest.Objects {
		got, err := restored.HeadObject(ctx, object.Bucket, object.Key)
		if err != nil {
			t.Fatal(err)
		}
		if got.ContentType != object.ContentType || !reflect.DeepEqual(got.Metadata, object.Metadata) {
			t.Fatalf("restored object = %+v", got)
		}
	}
	buckets, err := restored.ListBuckets(ctx)
	if err != nil || len(buckets) != 3 {
		t.Fatalf("buckets=%v %v", buckets, err)
	}
	info, err := os.Stat(filepath.Join(restored.Dir(), "normal.txt"))
	if err != nil || info.Mode().Perm() != 0o660 {
		t.Fatalf("mode=%v %v", info, err)
	}
}

func assertPortableRestoredLineage(t *testing.T, restored *Workspace, remote, origin string) {
	t.Helper()
	ctx := context.Background()
	second, err := restored.CreateCheckpoint(ctx, CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := LoadCheckpoint(remote, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ParentID != "" || resumed.Provenance.OriginCheckpointID != origin {
		t.Fatalf("lineage=%+v", resumed)
	}
}

func TestPortableMetadataDiffAndDeltaRefusal(t *testing.T) {
	w, registry := portableFixture(t)
	ctx := context.Background()
	first, err := w.CreateCheckpoint(ctx, CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.PutObject(ctx, "primary", "normal.txt", strings.NewReader("hello world"), storage.PutOptions{ContentType: "application/custom", Metadata: map[string]string{"project": "changed"}}); err != nil {
		t.Fatal(err)
	}
	second, err := w.CreateCheckpoint(ctx, CheckpointOptions{PortableObjects: true, ParentID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := ComparePortableCheckpoints(registry, first.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Objects) != 1 || diff.Objects[0].Kind != "changed" {
		t.Fatalf("diff=%+v", diff)
	}
	if _, err := CompareCheckpoints(registry, first.ID, second.ID); err == nil {
		t.Fatal("file-only comparison accepted portable objects")
	}
	if _, err := CreateDelta(ctx, registry, first.ID, second.ID, DeltaOptions{}); err == nil {
		t.Fatal("delta accepted portable objects")
	}
}

func TestPortableReplayVerifiesDedicatedPayload(t *testing.T) {
	w, registry := portableFixture(t)
	ctx := context.Background()
	request := CheckpointRequest{Key: "portable-turn", Options: CheckpointOptions{PortableObjects: true}}
	result, err := CaptureCheckpoint(ctx, registry, w.ID(), request)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadCheckpoint(registry, result.Checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(registry, "checkpoints", manifest.ID, "objects", manifest.Payloads[0].Path)
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveCheckpoint(ctx, registry, w.ID(), request); err == nil {
		t.Fatal("replay accepted corrupted object")
	}
}
