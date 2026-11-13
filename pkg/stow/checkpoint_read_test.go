package stow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

func checkpointReaderFixture(t *testing.T, files map[string][]byte, options CheckpointOptions) (*Workspace, string, CheckpointInfo) {
	t.Helper()
	registryDir := filepath.Join(t.TempDir(), "registry")
	root := filepath.Join(t.TempDir(), "work")
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: root, RegistryDir: registryDir, Bucket: "checkpoint-reader"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint, err := ws.CreateCheckpoint(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	return ws, registryDir, checkpoint
}

func openCheckpointReaderForTest(t *testing.T, registryDir, workspaceID, checkpointID string, roots []string) *CheckpointReader {
	t.Helper()
	reader, err := OpenCheckpointReader(context.Background(), CheckpointReaderOptions{
		RegistryDir: registryDir, WorkspaceID: workspaceID, CheckpointID: checkpointID, ReadRoots: roots,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	return reader
}

func TestCheckpointReaderReturnsSavedView(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{
		"docs/readme.txt": []byte("saved snapshot"), "docs/nested/guide.txt": []byte("guide"), "top.txt": []byte("top"),
	}, CheckpointOptions{})
	if err := os.WriteFile(filepath.Join(ws.Dir(), "docs", "readme.txt"), []byte("live edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	result, err := reader.ReadFile(context.Background(), "docs/readme.txt")
	if err != nil || string(result.Data) != "saved snapshot" || result.Path != "docs/readme.txt" || result.Offset != 0 || result.Size != 14 {
		t.Fatalf("saved file = %+v, %v", result, err)
	}
	if result.Path != "docs/readme.txt" || result.Offset != 0 || result.Size != 14 {
		t.Fatalf("saved file metadata = %+v", result)
	}
}

func TestCheckpointReaderReturnsEmptyFile(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"empty.txt": nil}, CheckpointOptions{})
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	empty, err := reader.ReadFile(context.Background(), "empty.txt")
	if err != nil || len(empty.Data) != 0 || empty.Size != 0 || empty.SHA256 != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty saved file = %+v, %v", empty, err)
	}
}

func TestCheckpointReaderReferenceCopiesReadRoots(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"file.txt": []byte("saved")}, CheckpointOptions{})
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	reference := reader.Reference()
	if len(reference.ReadRoots) != 1 || reference.ReadRoots[0] != "." {
		t.Fatalf("reference roots = %v", reference.ReadRoots)
	}
	reference.ReadRoots[0] = "changed"
	if reader.Reference().ReadRoots[0] != "." {
		t.Fatal("reference returned the reader's retained root slice")
	}
}

func TestCheckpointReaderPagesImplicitDirectories(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{
		"docs/readme.txt": []byte("saved snapshot"), "docs/nested/guide.txt": []byte("guide"), "top.txt": []byte("top"),
	}, CheckpointOptions{})
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	first, err := reader.ReadDirectory(context.Background(), ".", 0, 1)
	if err != nil || first.Total != 2 || len(first.Entries) != 1 || first.Entries[0].Name != "docs" {
		t.Fatalf("first root page = %+v, %v", first, err)
	}
	last, err := reader.ReadDirectory(context.Background(), ".", first.Offset+len(first.Entries), 2)
	if err != nil || last.Offset != 1 || last.Total != 2 || len(last.Entries) != 1 || last.Entries[0].Name != "top.txt" {
		t.Fatalf("last root page = %+v, %v", last, err)
	}
	children, err := reader.ReadDirectory(context.Background(), "docs", 0, 100)
	if err != nil || len(children.Entries) != 2 || children.Entries[0].Name != "nested" {
		t.Fatalf("implicit directory page = %+v, %v", children, err)
	}
}

func TestCheckpointReaderVerifiedRangeHashesWholePayload(t *testing.T) {
	body := make([]byte, 220_000)
	for index := range body {
		body[index] = byte(index * 31)
	}
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"large.bin": body}, CheckpointOptions{})
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	const offset, length = int64(65_531), int64(65_536)
	result, err := reader.ReadRange(context.Background(), "large.bin", offset, length)
	digest := sha256.Sum256(body)
	if err != nil || result.Offset != offset || result.Size != int64(len(body)) || string(result.Data) != string(body[offset:offset+length]) || result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("verified range = %+v, %v", result, err)
	}
	if _, err := reader.ReadRange(context.Background(), "large.bin", int64(len(body)), 1); err != nil {
		t.Fatalf("range at EOF should return an empty range: %v", err)
	}
	payload := filepath.Join(registryDir, "checkpoints", checkpoint.ID, "files", "large.bin")
	if err := os.Chmod(payload, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(payload, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("x"), int64(len(body)-1)); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(payload, 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadRange(context.Background(), "large.bin", 0, 16); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("corruption outside requested range = %v", err)
	}
}

func TestCheckpointReaderRootPrivacyAndBounds(t *testing.T) {
	files := map[string][]byte{"docs/readme.txt": []byte("ok"), "docs-private/no.txt": []byte("hidden"),
		".env": []byte("secret"), ".aws/credentials": []byte("secret"), "credentialſ/secret.txt": []byte("secret")}
	ws, registryDir, checkpoint := checkpointReaderFixture(t, files, CheckpointOptions{IncludeSensitiveFiles: true})
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, []string{"docs"})
	if _, err := reader.ReadFile(context.Background(), "docs-private/no.txt"); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("sibling scope read = %v", err)
	}
	if _, err := reader.ReadFile(context.Background(), ".env"); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("private file read = %v", err)
	}
	if _, err := reader.ReadDirectory(context.Background(), ".", 0, 0); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("invalid directory page = %v", err)
	}
	if _, err := reader.ReadRange(context.Background(), "docs/readme.txt", -1, 1); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("negative range = %v", err)
	}
	if _, err := OpenCheckpointReader(context.Background(), CheckpointReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID, ReadRoots: []string{}}); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("empty root list = %v", err)
	}
	if _, err := OpenCheckpointReader(context.Background(), CheckpointReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID, ReadRoots: []string{"missing"}}); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("missing root = %v", err)
	}
	entries, err := reader.ReadDirectory(context.Background(), "docs", 0, 100)
	if err != nil || len(entries.Entries) != 1 || entries.Entries[0].Name != "readme.txt" {
		t.Fatalf("scoped listing = %+v, %v", entries, err)
	}
	all := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	root, err := all.ReadDirectory(context.Background(), ".", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range root.Entries {
		if entry.Name == ".env" || entry.Name == ".aws" || entry.Name == "credentialſ" {
			t.Fatalf("private path visible in checkpoint tree: %+v", entry)
		}
	}
}

func TestCheckpointReaderSupportsPortableV2FileView(t *testing.T) {
	ws, registryDir := portableFixture(t)
	checkpoint, err := ws.CreateCheckpoint(context.Background(), CheckpointOptions{PortableObjects: true})
	if err != nil {
		t.Fatal(err)
	}
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	if reader.Reference().Version != 2 {
		t.Fatalf("portable checkpoint version = %d", reader.Reference().Version)
	}
	if _, err := reader.ReadFile(context.Background(), "normal.txt"); err != nil {
		t.Fatalf("read manifest.Files path from portable checkpoint: %v", err)
	}
	if _, err := reader.ReadFile(context.Background(), "objects/secret"); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("logical object namespace appeared as a file: %v", err)
	}
}

func TestCheckpointReaderExpiryManifestReplacementAndClose(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"file.txt": []byte("saved")}, CheckpointOptions{})
	reader, err := OpenCheckpointReader(context.Background(), CheckpointReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID, Lifetime: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(registryDir, "checkpoints", checkpoint.ID, "manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(manifest, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadFile(context.Background(), "file.txt"); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("changed manifest read = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadFile(context.Background(), "file.txt"); !errors.Is(err, ErrCheckpointReadClosed) {
		t.Fatalf("closed reader error = %v", err)
	}
	idle, err := OpenCheckpointReader(context.Background(), CheckpointReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID, Lifetime: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if _, err := idle.ReadFile(context.Background(), "file.txt"); !errors.Is(err, ErrCheckpointReadExpired) {
		t.Fatalf("idle expired reader error = %v", err)
	}
	removal, err := workspace.AcquireCheckpointRemoval(registryDir, checkpoint.ID)
	if err != nil {
		t.Fatalf("expiry timer did not release checkpoint lease: %v", err)
	}
	_ = removal.Release()
}

func TestCheckpointReaderDetectsCheckpointRootReplacement(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"file.txt": []byte("saved")}, CheckpointOptions{})
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	checkpointDir := filepath.Join(registryDir, "checkpoints", checkpoint.ID)
	replacedDir := checkpointDir + "-old"
	if err := os.Rename(checkpointDir, replacedDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(checkpointDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadFile(context.Background(), "file.txt"); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("read after root replacement = %v", err)
	}
}

func TestCheckpointReaderActiveOperationExpires(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"file.txt": []byte("saved")}, CheckpointOptions{})
	reader, err := OpenCheckpointReader(context.Background(), CheckpointReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID, Lifetime: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	reader.mu.Lock()
	opCtx, cancel, err := reader.operationContext(context.Background())
	if err != nil {
		reader.mu.Unlock()
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	operationErr := reader.operationError(opCtx, opCtx.Err())
	reader.mu.Unlock()
	cancel()
	if !errors.Is(operationErr, ErrCheckpointReadExpired) {
		t.Fatalf("active operation deadline = %v", operationErr)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointReaderDeletionLeasesAndCaptureCoexistence(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"file.txt": []byte("saved")}, CheckpointOptions{})
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	if _, err := CheckpointOf(context.Background(), registryDir, ws.ID(), CheckpointOptions{}); err != nil {
		t.Fatalf("capture must coexist with checkpoint reader: %v", err)
	}
	if err := DeleteCheckpoint(context.Background(), registryDir, checkpoint.ID); !errors.Is(err, ErrCheckpointInUse) {
		t.Fatalf("direct deletion error = %v", err)
	}
	registry, err := workspace.OpenRegistry(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ForgetCheckpoints(ws.ID()); !errors.Is(err, workspace.ErrCheckpointInUse) {
		t.Fatalf("bulk deletion error = %v", err)
	}
	if err := registry.RemoveWorkspace(ws.ID(), func() error { return nil }); !errors.Is(err, workspace.ErrWorkspaceInUse) {
		t.Fatalf("workspace removal error = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCheckpoint(context.Background(), registryDir, checkpoint.ID); err != nil {
		t.Fatalf("direct deletion after close: %v", err)
	}
}

func TestCheckpointReaderSetupFailureReleasesBothLeases(t *testing.T) {
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"file.txt": []byte("saved")}, CheckpointOptions{})
	manifestPath := filepath.Join(registryDir, "checkpoints", checkpoint.ID, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCheckpointReader(context.Background(), CheckpointReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID}); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("corrupt manifest setup error = %v", err)
	}
	reader, err := workspace.AcquireCheckpointRemoval(registryDir, checkpoint.ID)
	if err != nil {
		t.Fatalf("checkpoint lease after setup failure: %v", err)
	}
	_ = reader.Release()
	registry, err := workspace.OpenRegistry(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RemoveWorkspace(ws.ID(), func() error { return nil }); err != nil {
		t.Fatalf("workspace lease after setup failure: %v", err)
	}
}

func TestCheckpointReaderRejectsTooLargeWholeReadAndCopiesData(t *testing.T) {
	body := bytes.Repeat([]byte("x"), checkpointReadMaxFileBytes+1)
	ws, registryDir, checkpoint := checkpointReaderFixture(t, map[string][]byte{"large.txt": body}, CheckpointOptions{})
	reader := openCheckpointReaderForTest(t, registryDir, ws.ID(), checkpoint.ID, nil)
	if _, err := reader.ReadFile(context.Background(), "large.txt"); !errors.Is(err, ErrCheckpointReadTooLarge) {
		t.Fatalf("oversize whole read = %v", err)
	}
	if _, err := reader.ReadRange(context.Background(), "large.txt", 0, checkpointReadMaxFileBytes+1); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("oversize range request = %v", err)
	}
	if _, err := reader.ReadRange(context.Background(), "large.txt", int64(len(body))+1, 1); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("past EOF range request = %v", err)
	}
	if _, err := OpenCheckpointReader(context.Background(), CheckpointReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID, MaxVerifyBytes: checkpointReadMaxVerify + 1}); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("oversize verify bound = %v", err)
	}
}

func TestCheckpointReadIndexRejectsFileDirectoryCollision(t *testing.T) {
	files := []CheckpointFile{
		{Path: "tree", Size: 0, Mode: 0o600, SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{Path: "tree/child", Size: 0, Mode: 0o600, SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	if _, _, _, err := indexCheckpointReadFiles(files, nil); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("file/directory collision = %v", err)
	}
}

func TestCheckpointHashStopsAtManifestSizePlusOne(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "growing-payload")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(bytes.Repeat([]byte("x"), 2<<20)); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := hashCheckpointPayload(context.Background(), file, 0, 1, 1); !errors.Is(err, ErrCheckpointReadUnavailable) {
		t.Fatalf("growing payload error = %v", err)
	}
	position, err := file.Seek(0, 1)
	if err != nil || position != 2 {
		t.Fatalf("hash read %d bytes beyond its manifest size, %v", position-1, err)
	}
}

func TestCheckpointHashCancellationDiscardsCapturedWindow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterRead{reader: bytes.NewReader(bytes.Repeat([]byte("x"), 200_000)), cancel: cancel, cancelAfter: 2}
	data, _, _, err := hashCheckpointPayload(ctx, reader, 65_536, 32, 200_000)
	if !errors.Is(err, context.Canceled) || data != nil {
		t.Fatalf("cancelled payload returned %d bytes and error %v", len(data), err)
	}
}

type cancelAfterRead struct {
	reader      io.Reader
	cancel      context.CancelFunc
	reads       int
	cancelAfter int
}

func (r *cancelAfterRead) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	if count > 0 {
		r.reads++
	}
	if r.reads == r.cancelAfter {
		r.cancel()
	}
	return count, err
}
