package fs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type capacityRepeatReader struct{}

func (capacityRepeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'q'
	}
	return len(p), nil
}

func capacityPayloadFixture(t testing.TB, root, kind string, size int64) (*FilesystemStore, *capacity.Namespace, string) {
	t.Helper()
	if !(&FilesystemStore{}).SupportsGuardedWrites() {
		t.Skip("namespace capacity admission requires Darwin or Linux")
	}
	var dir string
	if root == "" {
		dir = t.TempDir()
	} else {
		var err error
		dir, err = os.MkdirTemp(root, "payload-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	store, err := NewFilesystemStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	var path string
	if kind == "object" {
		_, err = store.PutObject(ctx, "bucket", "payload", io.LimitReader(capacityRepeatReader{}, size), storage.PutOptions{ContentType: "application/octet-stream", Metadata: map[string]string{"label": "東京"}})
		path = store.objectPath("bucket", "payload")
	} else {
		var upload *storage.MultipartUpload
		upload, err = store.CreateMultipartUpload(ctx, "bucket", "payload", storage.MultipartOptions{})
		if err == nil {
			path = filepath.Join(store.multipartDir(upload.UploadID), "part-00001")
			err = atomicfile.WriteStream(path, 0644, nil, io.LimitReader(capacityRepeatReader{}, size))
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	host, err := capacity.Open(capacity.HostOptions{Dir: filepath.Join(dir, "host"), MaxBytes: 2 << 30})
	if err != nil {
		t.Fatal(err)
	}
	ns, err := host.Bind(capacity.NamespaceOptions{ID: "payload", MaxBytes: 1 << 30, RecoveryReserveBytes: 128 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return store, ns, path
}

func expectedPayloadResource(t testing.TB, path string) capacity.Resource {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	h := sha256.New()
	n, err := io.Copy(h, file)
	if err != nil {
		t.Fatal(err)
	}
	id := sha256.Sum256([]byte(filepath.Clean(path)))
	return capacity.Resource{ID: hex.EncodeToString(id[:]), Version: hex.EncodeToString(h.Sum(nil)), Bytes: n, Class: capacity.Payload}
}

func assertPayloadResource(t testing.TB, store *FilesystemStore, want capacity.Resource) {
	t.Helper()
	usage, err := store.scanCapacityLocked()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, got := range usage.Resources {
		if got.ID == want.ID {
			found = true
			if got != want {
				t.Fatalf("payload identity changed: got=%+v want=%+v", got, want)
			}
		}
	}
	if !found {
		t.Fatal("payload resource missing")
	}
}

func TestCapacityPayloadAllocationBound(t *testing.T) {
	for _, kind := range []string{"object", "part"} {
		for _, size := range []int64{8 << 20, 64 << 20} {
			t.Run(fmt.Sprintf("%s/%d", kind, size), func(t *testing.T) {
				store, ns, path := capacityPayloadFixture(t, "", kind, size)
				want := expectedPayloadResource(t, path)
				before, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				runtime.GC()
				var a, b runtime.MemStats
				runtime.ReadMemStats(&a)
				err = store.BindNamespace(ns)
				runtime.ReadMemStats(&b)
				if err != nil {
					t.Fatal(err)
				}
				allocated := b.TotalAlloc - a.TotalAlloc
				t.Logf("kind=%s payload=%d binding allocated=%d budget=%d", kind, size, allocated, 4<<20)
				if allocated > 4<<20 {
					t.Errorf("capacity binding materializes payload: kind=%s size=%d allocated=%d", kind, size, allocated)
				}
				after, err := os.Stat(path)
				if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("binding changed payload file: %v", err)
				}
				assertPayloadResource(t, store, want)
				snapshot, err := ns.Snapshot(context.Background())
				if err != nil || snapshot.PayloadBytes != want.Bytes {
					t.Fatalf("payload charge=%+v want=%d err=%v", snapshot, want.Bytes, err)
				}
			})
		}
	}
}

func TestCapacityPayloadIdentities(t *testing.T) {
	for _, kind := range []string{"object", "part"} {
		for _, size := range []int64{0, 257} {
			t.Run(fmt.Sprintf("%s/%d", kind, size), func(t *testing.T) {
				store, _, path := capacityPayloadFixture(t, "", kind, size)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				name := sha256.Sum256([]byte(filepath.Clean(path)))
				sum := sha256.Sum256(data)
				want := capacity.Resource{ID: hex.EncodeToString(name[:]), Version: hex.EncodeToString(sum[:]), Bytes: int64(len(data)), Class: capacity.Payload}
				assertPayloadResource(t, store, want)
			})
		}
	}
	t.Run("legacy", func(t *testing.T) {
		store, _, path := capacityPayloadFixture(t, "", "object", 3)
		data, err := json.Marshal(objectRecord{Version: 1, Data: []byte("legacy"), ETag: "tag", LastModified: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		name := sha256.Sum256([]byte(filepath.Clean(path)))
		sum := sha256.Sum256(data)
		assertPayloadResource(t, store, capacity.Resource{ID: hex.EncodeToString(name[:]), Version: hex.EncodeToString(sum[:]), Bytes: int64(len(data)), Class: capacity.Payload})
	})
}

func TestCapacityPayloadInvalidRecords(t *testing.T) {
	store, _, path := capacityPayloadFixture(t, "", "object", 16)
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	badSize := bytes.Clone(valid)
	binary.LittleEndian.PutUint32(badSize[len(binaryRecordMagic):], ^uint32(0))
	for name, data := range map[string][]byte{"prefix": valid[:7], "header": badSize, "short": valid[:len(valid)-1], "long": append(bytes.Clone(valid), 1)} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := store.scanCapacityLocked(); err == nil {
				t.Fatal("malformed object accepted")
			}
		})
	}
}

func TestCapacityPayloadNativeRefusals(t *testing.T) {
	t.Run("permission", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("requires enforced non-root permissions")
		}
		store, _, path := capacityPayloadFixture(t, "", "object", 16)
		t.Cleanup(func() { _ = os.Chmod(path, 0644) })
		if err := os.Chmod(path, 0000); err != nil {
			t.Fatal(err)
		}
		_, err := store.scanCapacityLocked()
		var native *os.PathError
		if !errors.Is(err, os.ErrPermission) || !errors.As(err, &native) || native.Op != "open" || native.Path != path {
			t.Fatalf("native permission error changed: %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		store, _, path := capacityPayloadFixture(t, "", "object", 16)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), path); err != nil {
			t.Fatal(err)
		}
		if _, err := store.scanCapacityLocked(); !errors.Is(err, capacity.ErrUnknown) {
			t.Fatalf("symlink accepted: %v", err)
		}
	})
}

func TestCapacityPayloadUnknownAllocation(t *testing.T) {
	store, _, _ := capacityPayloadFixture(t, "", "object", 16)
	path := filepath.Join(store.dataDir, "unexpected")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	_, err = store.scanCapacityLocked()
	runtime.ReadMemStats(&b)
	if !errors.Is(err, capacity.ErrUnknown) {
		t.Fatalf("unknown file result=%v", err)
	}
	allocated := b.TotalAlloc - a.TotalAlloc
	t.Logf("unknown bytes=%d allocated=%d", 64<<20, allocated)
	if allocated > 4<<20 {
		t.Errorf("unknown scan file was materialized: allocated=%d", allocated)
	}
}
