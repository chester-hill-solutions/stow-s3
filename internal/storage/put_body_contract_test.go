package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type borrowedPutReader struct {
	*bytes.Reader
	data []byte
}

func (r borrowedPutReader) Bytes() []byte { return r.data }

func TestStorePreparesEmptyBody(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		if err := store.CreateBucket(ctx, "contract"); err != nil {
			t.Fatal(err)
		}
		meta, err := store.PutObject(ctx, "contract", "empty", nil, storage.PutOptions{ComputeChecksum: true, ChecksumAlgorithm: "CRC32"})
		if err != nil {
			t.Fatal(err)
		}
		if meta.Size != 0 || meta.ETag != storage.ETagForBytes(nil) || meta.ChecksumValue != "AAAAAA==" {
			t.Fatalf("empty object metadata: %+v", meta)
		}
		reader, _, err := store.GetObject(ctx, "contract", "empty")
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || len(data) != 0 {
			t.Fatalf("empty object body: %q, %v", data, err)
		}
	})
}

func TestStorePreparesOwnedChecksumBody(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		if err := store.CreateBucket(ctx, "contract"); err != nil {
			t.Fatal(err)
		}
		for _, algorithm := range []string{"", "crc32", "crc32c", "crc64nvme", "sha1", "sha256"} {
			data := []byte("caller payload")
			metadata := map[string]string{"state": "original"}
			key := "prepared-" + algorithm
			meta, err := store.PutObject(ctx, "contract", key, borrowedPutReader{bytes.NewReader(data), data}, storage.PutOptions{ComputeChecksum: true, ChecksumAlgorithm: algorithm, Metadata: metadata})
			if err != nil {
				t.Fatal(err)
			}
			checksum := ""
			if algorithm != "" {
				checksum, err = storage.ComputeChecksum(algorithm, data)
				if err != nil {
					t.Fatal(err)
				}
			}
			if meta.ETag != storage.ETagForBytes(data) || meta.Size != int64(len(data)) || meta.ChecksumAlgorithm != strings.ToUpper(algorithm) || meta.ChecksumValue != checksum {
				t.Fatalf("prepared metadata for %q: %+v", algorithm, meta)
			}
			data[0] = 'X'
			metadata["state"] = "caller mutation"
			meta.Metadata["state"] = "result mutation"
			reader, saved, err := store.GetObject(ctx, "contract", key)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(body) != "caller payload" || saved.Metadata["state"] != "original" || saved.ChecksumValue != checksum {
				t.Fatalf("caller mutation changed saved object: %q %+v %v", body, saved, err)
			}
		}
	})
}

type interruptedPutReader struct{}

func (interruptedPutReader) Read(p []byte) (int, error) {
	return copy(p, "partial"), io.ErrUnexpectedEOF
}

func TestStorePreparationFailurePreservesObject(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		if err := store.CreateBucket(ctx, "contract"); err != nil {
			t.Fatal(err)
		}
		original, err := store.PutObject(ctx, "contract", "object", bytes.NewBufferString("original"), storage.PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, attempt := range []struct {
			body io.Reader
			opts storage.PutOptions
			want error
		}{
			{interruptedPutReader{}, storage.PutOptions{}, io.ErrUnexpectedEOF},
			{bytes.NewBufferString("replacement"), storage.PutOptions{ComputeChecksum: true, ChecksumAlgorithm: "SHA256", ChecksumValue: "wrong"}, storage.ErrChecksumMismatch},
		} {
			if _, err := store.PutObject(ctx, "contract", "object", attempt.body, attempt.opts); !errors.Is(err, attempt.want) {
				t.Fatalf("preparation error: %v, want %v", err, attempt.want)
			}
			current, err := store.HeadObject(ctx, "contract", "object")
			if err != nil || current.VersionID != original.VersionID {
				t.Fatalf("failed preparation replaced object: %+v, %v", current, err)
			}
		}
	})
}
