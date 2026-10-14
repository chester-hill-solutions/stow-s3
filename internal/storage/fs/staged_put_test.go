package fs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func stagedStore(t *testing.T) *FilesystemStore {
	t.Helper()
	s, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.CreateBucket(context.Background(), "assets"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDiskStageOwnsBytesAndChecksums(t *testing.T) {
	ctx := context.Background()
	s := stagedStore(t)
	for _, owned := range []bool{false, true} {
		data := []byte("payload")
		metadata := map[string]string{"state": "original"}
		var body io.Reader = bytes.NewReader(data)
		if owned {
			body = storage.NewOwnedBody(data)
		}
		pending, err := s.StageObject(ctx, "assets", "key", body, storage.PutOptions{ComputeChecksum: true, ChecksumAlgorithm: " sha256 ", Metadata: metadata})
		if err != nil {
			t.Fatal(err)
		}
		if !owned {
			data[0] = 'X'
		}
		metadata["state"] = "changed"
		meta, err := pending.Commit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pending.Commit(ctx); err == nil {
			t.Fatal("stage committed twice")
		}
		if err := pending.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		reader, saved, err := s.GetObject(ctx, "assets", "key")
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		_ = reader.Close()
		checksum, _ := storage.ComputeChecksum("SHA256", []byte("payload"))
		if err != nil || string(got) != "payload" || saved.Metadata["state"] != "original" || saved.ChecksumValue != checksum || meta.Size != 7 {
			t.Fatalf("data=%q meta=%+v err=%v", got, saved, err)
		}
	}
	assertNoStageFiles(t, s)
}

func TestDiskStageRechecksConditionsAndCleansAbort(t *testing.T) {
	ctx := context.Background()
	s := stagedStore(t)
	pending, err := s.StageObject(ctx, "assets", "key", strings.NewReader("staged"), storage.PutOptions{IfNoneMatch: "*"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.HeadObject(ctx, "assets", "key"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatal("uncommitted stage visible")
	}
	if _, err := s.PutObject(ctx, "assets", "key", strings.NewReader("winner"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Commit(ctx); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("stale stage: %v", err)
	}
	pending, err = s.StageObject(ctx, "assets", "other", strings.NewReader("staged"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := pending.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Commit(ctx); err == nil {
		t.Fatal("aborted stage committed")
	}
	if err := pending.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	assertNoStageFiles(t, s)
}

func TestDiskStageFailurePreservesPublishedObject(t *testing.T) {
	ctx := context.Background()
	s := stagedStore(t)
	if _, err := s.PutObject(ctx, "assets", "key", strings.NewReader("original"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	checksum, _ := storage.ComputeChecksum("SHA256", []byte("different"))
	if _, err := s.StageObject(ctx, "assets", "key", strings.NewReader("corrupt"), storage.PutOptions{ChecksumAlgorithm: "SHA256", ChecksumValue: checksum}); !errors.Is(err, storage.ErrChecksumMismatch) {
		t.Fatalf("checksum: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	pending, err := s.StageObject(cancelled, "assets", "key", strings.NewReader("cancelled"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := pending.Commit(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	pending, err = s.StageObject(ctx, "assets", "key", strings.NewReader("closed"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Commit(ctx); err == nil {
		t.Fatal("stage committed after close")
	}
	assertNoStageFiles(t, s)
	reopened, err := NewFilesystemStore(s.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reader, _, err := reopened.GetObject(ctx, "assets", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != "original" {
		t.Fatalf("published %q %v", got, err)
	}
}

func assertNoStageFiles(t *testing.T, s *FilesystemStore) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(s.dataDir, ".tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("stage litter %v %v", matches, err)
	}
}

func TestDiskStaleStageRemovedOnReopen(t *testing.T) {
	s := stagedStore(t)
	path := filepath.Join(s.dataDir, ".tmp-body-crash")
	if err := os.WriteFile(path, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFilesystemStore(s.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale stage retained: %v", err)
	}
}

func TestDiskStagePreventsCapacityRebindingUntilFinished(t *testing.T) {
	s := stagedStore(t)
	ctx := context.Background()
	pending, err := s.StageObject(ctx, "assets", "key", strings.NewReader("staged"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	namespace := publicationNamespace(t)
	if err := s.BindNamespace(namespace); err == nil {
		t.Fatal("capacity binding changed during staging")
	}
	if err := pending.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	if s.activeStages != 0 {
		t.Fatal("staging admission leaked")
	}
	if err := s.BindNamespace(namespace); err != nil {
		t.Fatal(err)
	}
}

func TestDiskStageVerifiesLargeStreamChecksums(t *testing.T) {
	s := stagedStore(t)
	ctx := context.Background()
	data := bytes.Repeat([]byte("checksummed"), 16<<10)
	for _, algorithm := range []string{"CRC32", "CRC32C", "CRC64NVME", "SHA1", "SHA256"} {
		t.Run(algorithm, func(t *testing.T) {
			meta, err := s.PutObject(ctx, "assets", "key", bytes.NewReader(data), storage.PutOptions{ComputeChecksum: true, ChecksumAlgorithm: algorithm})
			if err != nil {
				t.Fatal(err)
			}
			expected, err := storage.ComputeChecksum(algorithm, data)
			if err != nil {
				t.Fatal(err)
			}
			if meta.ChecksumValue != expected || meta.ETag != storage.ETagForBytes(data) {
				t.Fatal("streamed checksum or ETag differs")
			}
			if _, err := s.PutObject(ctx, "assets", "key", bytes.NewReader(data), storage.PutOptions{ChecksumAlgorithm: algorithm, ChecksumValue: "wrong"}); !errors.Is(err, storage.ErrChecksumMismatch) {
				t.Fatalf("checksum mismatch: %v", err)
			}
		})
	}
	assertNoStageFiles(t, s)
}
