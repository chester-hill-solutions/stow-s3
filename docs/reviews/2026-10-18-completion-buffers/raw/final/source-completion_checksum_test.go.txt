package fs_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestCompletionBufferChecksums(t *testing.T) {
	for _, algorithm := range []string{"CRC32", "CRC32C", "CRC64NVME", "SHA1", "SHA256"} {
		t.Run(algorithm, func(t *testing.T) { completeChecksumParts(t, algorithm) })
	}
}

func completeChecksumParts(t *testing.T, algorithm string) {
	t.Helper()
	ctx := context.Background()
	store, _, _ := partStore(t, false, 0)
	first := bytes.Repeat([]byte("q"), storage.MinPartSize)
	tail := "a different tail"
	expected, err := storage.ComputeChecksumReader(algorithm, io.MultiReader(bytes.NewReader(first), strings.NewReader(tail)))
	if err != nil {
		t.Fatal(err)
	}
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "payload", storage.MultipartOptions{ChecksumAlgorithm: algorithm, ChecksumValue: expected})
	if err != nil {
		t.Fatal(err)
	}
	var parts []storage.PartInfo
	for i, body := range []io.Reader{bytes.NewReader(first), strings.NewReader(tail)} {
		part, e := store.UploadPart(ctx, upload.UploadID, i+1, body)
		if e != nil {
			t.Fatal(e)
		}
		parts = append(parts, *part)
	}
	meta, err := store.CompleteMultipartUpload(ctx, upload.UploadID, parts)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ChecksumValue != expected || meta.ChecksumAlgorithm != algorithm || meta.Size != int64(len(first)+len(tail)) {
		t.Fatalf("completion checksum/size changed: %+v", meta)
	}
	reader, _, err := store.GetObject(ctx, "bucket", "payload")
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := storage.ComputeChecksumReader(algorithm, reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || actual != expected {
		t.Fatalf("published part order/bytes changed: %s %v %v", actual, readErr, closeErr)
	}
}
