package fs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestBinaryRecordRoundTrip(t *testing.T) {
	store := recordStore(t)
	body := bytes.Repeat([]byte{0, 255, 128, 1}, 4096)
	options := storage.PutOptions{ContentType: "application/octet-stream", Metadata: map[string]string{"label": "日本語"}}
	original := putRecord(t, store, "key", body, options)
	onDisk, err := os.ReadFile(store.objectPath("bucket", "key"))
	if err != nil || !bytes.HasPrefix(onDisk, []byte(binaryRecordMagic)) || !bytes.HasSuffix(onDisk, body) {
		t.Fatalf("payload is not stored raw: %v", err)
	}
	meta, err := store.HeadObject(context.Background(), "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ETag != original.ETag || meta.Size != int64(len(body)) || meta.Metadata["label"] != "日本語" {
		t.Fatalf("metadata=%+v", meta)
	}
	assertStoredBody(t, store, "key", body)
}

func TestOpenRecordSurvivesOverwriteAndDelete(t *testing.T) {
	store := recordStore(t)
	body := []byte("captured version")
	putRecord(t, store, "key", body, storage.PutOptions{})
	reader, _, err := store.GetObject(context.Background(), "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	putRecord(t, store, "key", []byte("replacement"), storage.PutOptions{})
	if err := store.DeleteObject(context.Background(), "bucket", "key"); err != nil {
		t.Fatal(err)
	}
	old, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(old, body) {
		t.Fatalf("open reader lost captured version: %v", err)
	}
}

func TestBinaryRecordSurvivesReopen(t *testing.T) {
	store := recordStore(t)
	putRecord(t, store, "key", []byte("persisted"), storage.PutOptions{})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFilesystemStore(store.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertStoredBody(t, reopened, "key", []byte("persisted"))
}

func recordStore(t *testing.T) *FilesystemStore {
	t.Helper()
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatal(err)
	}
	return store
}

func putRecord(t *testing.T, store *FilesystemStore, key string, body []byte, options storage.PutOptions) *storage.ObjectMeta {
	t.Helper()
	meta, err := store.PutObject(context.Background(), "bucket", key, bytes.NewReader(body), options)
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func TestLegacyJSONRecordReadableAndRewritten(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	record := objectRecord{Version: 1, Data: []byte("legacy"), ETag: "legacy-tag", LastModified: time.Now().UTC()}
	path := store.objectPath("bucket", "legacy")
	if s := store.isBoundedObjectPath("bucket", path); s {
		record.Key = "legacy"
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBytesAtomic(path, data); err != nil {
		t.Fatal(err)
	}
	assertStoredBody(t, store, "legacy", record.Data)
	page, err := store.ListObjectsV2(ctx, "bucket", storage.ListOptions{})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Size != 6 {
		t.Fatalf("legacy listing=%+v %v", page, err)
	}
	if _, err := store.PutObject(ctx, "bucket", "legacy", bytes.NewReader([]byte("new")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, []byte(binaryRecordMagic)) {
		t.Fatalf("legacy record was not rewritten: %v", err)
	}
	assertStoredBody(t, store, "legacy", []byte("new"))
}

func TestMalformedBinaryRecordRefused(t *testing.T) {
	record := objectRecord{Data: []byte("payload"), LastModified: time.Now().UTC()}
	valid, err := encodeObjectRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	oversized := bytes.Clone(valid)
	binary.LittleEndian.PutUint32(oversized[len(binaryRecordMagic):], ^uint32(0))
	wrongVersion := bytes.Clone(valid)
	wrongVersion[binaryRecordPrefixSize+1] = 3
	for name, data := range map[string][]byte{"prefix": valid[:7], "header-size": oversized, "truncated-payload": valid[:len(valid)-1], "extra-payload": append(bytes.Clone(valid), 1), "version": wrongVersion} {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir() + "/record"
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readObjectHeader(path); err == nil {
				t.Fatal("malformed record accepted")
			}
			if _, err := readObjectRecord(path); err == nil {
				t.Fatal("malformed payload accepted")
			}
		})
	}
}

func assertStoredBody(t *testing.T, store *FilesystemStore, key string, want []byte) {
	t.Helper()
	reader, meta, err := store.GetObject(context.Background(), "bucket", key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, want) || meta.Size != int64(len(want)) {
		t.Fatalf("got %d bytes, metadata=%+v, error=%v", len(got), meta, err)
	}
}
