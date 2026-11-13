package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestPublishWorkspaceFilePreservesMetadataModeAndFreshGeneration(t *testing.T) {
	store, err := New(Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	checksum, err := storage.ComputeChecksum("SHA256", []byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.PutObject(ctx, "bucket", "note.txt", bytes.NewReader([]byte("old")), storage.PutOptions{ContentType: "text/custom", Metadata: map[string]string{"owner": "host"}, ChecksumAlgorithm: "SHA256", ChecksumValue: checksum})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, "note.txt")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	basis, err := store.ObserveWorkspaceFile(ctx, "bucket", "note.txt", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	basis.Data = nil
	publication, err := store.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "note.txt", Data: []byte("new"), Basis: &basis, MaxFileBytes: 64 << 10})
	if err != nil || publication.Outcome != storage.WorkspaceFileCommitted || !publication.IndexReady {
		t.Fatalf("publish=%+v error=%v", publication, err)
	}
	assertWorkspacePublishedMetadata(t, publication.Meta, old)
	assertWorkspacePublishedModeAndListing(t, store, path, publication.Meta)
}

func assertWorkspacePublishedMetadata(t *testing.T, meta, old *storage.ObjectMeta) {
	t.Helper()
	if meta.VersionID == old.VersionID || meta.ContentType != "text/custom" || meta.ChecksumAlgorithm != "SHA256" || meta.ChecksumValue == "" || meta.Metadata["owner"] != "host" || meta.ETag == old.ETag {
		t.Fatalf("publication metadata=%+v old=%+v", meta, old)
	}
}

func assertWorkspacePublishedModeAndListing(t *testing.T, store *Store, path string, meta *storage.ObjectMeta) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("saved mode=%v error=%v", info, err)
	}
	listed, err := store.ListObjectsV2(context.Background(), "bucket", storage.ListOptions{MaxKeys: 10})
	if err != nil || len(listed.Objects) != 1 || listed.Objects[0].VersionID != meta.VersionID {
		t.Fatalf("listing=%+v error=%v", listed, err)
	}
}

func TestPublishWorkspaceFileUnindexedBasisAndConcurrentConflict(t *testing.T) {
	store, err := New(Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	path := filepath.Join(store.root, "host.txt")
	if err := os.WriteFile(path, []byte("host"), 0o600); err != nil {
		t.Fatal(err)
	}
	basis, err := store.ObserveWorkspaceFile(context.Background(), "bucket", "host.txt", 64<<10)
	if err != nil || basis.Generation != "" {
		t.Fatalf("unindexed basis=%+v error=%v", basis, err)
	}
	basis.Data = nil
	request := storage.WorkspaceFileWriteRequest{Path: "host.txt", Data: []byte("saved"), Basis: &basis, MaxFileBytes: 64 << 10}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			publication, publishErr := store.PublishWorkspaceFile(context.Background(), "bucket", request)
			if publishErr == nil && publication.Outcome != storage.WorkspaceFileCommitted {
				publishErr = errors.New("publish was not committed")
			}
			results <- publishErr
		}()
	}
	wg.Wait()
	close(results)
	committed, conflicted := 0, 0
	for err := range results {
		if err == nil {
			committed++
		} else if errors.Is(err, storage.ErrWorkspaceFileConflict) {
			conflicted++
		} else {
			t.Fatalf("publish error: %v", err)
		}
	}
	if committed != 1 || conflicted != 1 {
		t.Fatalf("committed=%d conflicted=%d", committed, conflicted)
	}
}

func TestPublishWorkspaceFileRefusesMissingParentAndEnforcesPerFileBounds(t *testing.T) {
	store, err := New(Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "missing/leaf", Data: []byte("x"), Replace: true, MaxFileBytes: 8}); err == nil {
		t.Fatal("missing parent publish succeeded")
	}
	if _, err := os.Lstat(filepath.Join(store.root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing parent created: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.root, "small"), []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	basis, err := store.ObserveWorkspaceFile(ctx, "bucket", "small", 7)
	if err != nil {
		t.Fatal(err)
	}
	basis.Data = nil
	if _, err := store.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "small", Data: []byte("5678"), Basis: &basis, MaxFileBytes: 7}); err != nil {
		t.Fatalf("individually bounded replacement rejected: %v", err)
	}
	if _, err := store.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "small", Data: []byte("12345678"), Replace: true, MaxFileBytes: 7}); !errors.Is(err, storage.ErrWorkspaceFileTooLarge) {
		t.Fatalf("oversize new body error=%v", err)
	}
}

func TestWorkspaceFileIndexFailureBlocksFurtherObservations(t *testing.T) {
	store, err := New(Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.PutObject(ctx, "bucket", "note", bytes.NewReader([]byte("old")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	basis, err := store.ObserveWorkspaceFile(ctx, "bucket", "note", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	basis.Data = nil
	index := InternalPath(store.root, "index.json")
	backup := index + ".backup"
	if err := os.Rename(index, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0o700); err != nil {
		t.Fatal(err)
	}
	publication, publishErr := store.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "note", Data: []byte("new"), Basis: &basis, MaxFileBytes: 64 << 10})
	if publication.Outcome != storage.WorkspaceFileCommitted || publication.IndexReady || !errors.Is(publishErr, storage.ErrMutationCommitted) {
		t.Fatalf("publication=%+v error=%v", publication, publishErr)
	}
	if _, err := store.UsageRevision(ctx); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("usage revision error=%v", err)
	}
	if _, err := store.ObserveWorkspaceFile(ctx, "bucket", "note", 64<<10); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("observation after index failure=%v", err)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, index); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacePayloadWriteErrorAfterBytesChangeBlocksStore(t *testing.T) {
	store, err := New(Options{Root: t.TempDir(), Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.PutObject(ctx, "bucket", "note", bytes.NewReader([]byte("old")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	basis, err := store.ObserveWorkspaceFile(ctx, "bucket", "note", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	basis.Data = nil
	calls := 0
	store.fileSavePayloadWriter = func(path string, data []byte, mode os.FileMode) error {
		calls++
		if err := store.writeAtomicMode(path, data, mode, false); err != nil {
			return err
		}
		return errors.New("late atomic writer error")
	}
	publication, writeErr := store.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "note", Data: []byte("saved"), Basis: &basis, MaxFileBytes: 64 << 10})
	if publication.Outcome != storage.WorkspaceFileUnknown || !errors.Is(writeErr, storage.ErrSaveRequestUnknown) {
		t.Fatalf("late writer result=%+v error=%v", publication, writeErr)
	}
	data, err := os.ReadFile(filepath.Join(store.root, "note"))
	if err != nil || string(data) != "saved" {
		t.Fatalf("completed bytes=%q error=%v", data, err)
	}
	if revision, err := store.UsageRevision(ctx); revision != 1 || !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("blocked usage revision=%d error=%v", revision, err)
	}
	if _, err := store.ObserveWorkspaceFile(ctx, "bucket", "note", 64<<10); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("observation after unknown writer result=%v", err)
	}
	if _, err := store.PublishWorkspaceFile(ctx, "bucket", storage.WorkspaceFileWriteRequest{Path: "note", Data: []byte("again"), Replace: true, MaxFileBytes: 64 << 10}); !errors.Is(err, storage.ErrWorkspaceFileBlocked) || calls != 1 {
		t.Fatalf("publication after unknown result error=%v writer calls=%d", err, calls)
	}
}
