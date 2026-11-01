package fs_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	fsstore "github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type partRepeatReader struct{ remaining int64 }

func (r *partRepeatReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := range p[:n] {
		p[i] = 'q'
	}
	r.remaining -= n
	return int(n), nil
}
func partHashes(size int64) (string, string) {
	m, s := md5.New(), sha256.New()
	_, _ = io.Copy(io.MultiWriter(m, s), &partRepeatReader{remaining: size})
	return base64.StdEncoding.EncodeToString(m.Sum(nil)), hex.EncodeToString(s.Sum(nil))
}
func partStore(t *testing.T, bound bool, limit int64) (*fsstore.FilesystemStore, *capacity.Namespace, string) {
	t.Helper()
	if bound && !(&fsstore.FilesystemStore{}).SupportsGuardedWrites() {
		t.Skip("namespace capacity admission requires Darwin or Linux")
	}
	dir := t.TempDir()
	var ns *capacity.Namespace
	var err error
	if bound {
		host, e := capacity.Open(capacity.HostOptions{Dir: filepath.Join(dir, "host"), MaxBytes: 2 * limit})
		if e != nil {
			t.Fatal(e)
		}
		ns, err = host.Bind(capacity.NamespaceOptions{ID: "part", MaxBytes: limit, RecoveryReserveBytes: limit / 8})
		if err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "store")
	var store *fsstore.FilesystemStore
	if ns == nil {
		store, err = fsstore.NewFilesystemStore(path)
	} else {
		store, err = fsstore.NewFilesystemStoreWithNamespace(path, ns)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatal(err)
	}
	return store, ns, path
}
func partRequest(id string, size int64, md5value string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "http://localhost/bucket/payload?uploadId="+id+"&partNumber=1", &partRepeatReader{remaining: size})
	req.ContentLength = size
	req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
	req.Header.Set("Content-MD5", md5value)
	return req
}
func partUpload(t *testing.T, store *fsstore.FilesystemStore) *storage.MultipartUpload {
	t.Helper()
	u, err := store.CreateMultipartUpload(context.Background(), "bucket", "payload", storage.MultipartOptions{ContentType: "application/octet-stream", Metadata: map[string]string{"label": "part"}})
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func partCompleteAndVerify(t *testing.T, store *fsstore.FilesystemStore, id string, size int64, digest string) {
	t.Helper()
	ctx := context.Background()
	parts, err := store.ListParts(ctx, id)
	if err != nil || len(parts) != 1 || parts[0].Size != size {
		t.Fatalf("parts %+v %v", parts, err)
	}
	meta, err := store.CompleteMultipartUpload(ctx, id, parts)
	if err != nil || meta.Size != size || meta.Metadata["label"] != "part" || !storage.ETagEqual(meta.ETag, parts[0].ETag) {
		t.Fatalf("completion %+v %v", meta, err)
	}
	reader, _, err := store.GetObject(ctx, "bucket", "payload")
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	n, err := io.Copy(h, reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil || n != size || hex.EncodeToString(h.Sum(nil)) != digest {
		t.Fatal("part content changed", n, err, closeErr)
	}
}

type observingPartStore struct {
	*fsstore.FilesystemStore
	calls int
	owned bool
}

func (s *observingPartStore) UploadPart(ctx context.Context, id string, number int, body io.Reader) (*storage.PartInfo, error) {
	s.calls++
	_, s.owned = body.(*storage.OwnedBody)
	return s.FilesystemStore.UploadPart(ctx, id, number, body)
}
func TestMultipartOwnedHTTPTransfer(t *testing.T) {
	store, _, _ := partStore(t, false, 0)
	observed := &observingPartStore{FilesystemStore: store}
	upload := partUpload(t, store)
	server, err := s3api.New(s3api.Config{Store: observed, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	md5value, digest := partHashes(257)
	for _, invalid := range []string{"md5", "length"} {
		req := partRequest(upload.UploadID, 257, md5value)
		if invalid == "md5" {
			req.Header.Set("Content-MD5", "AAAAAAAAAAAAAAAAAAAAAA==")
		} else {
			req.ContentLength = 256
			req.Header.Set("Content-Length", "256")
		}
		out := httptest.NewRecorder()
		server.Handler().ServeHTTP(out, req)
		if out.Code != 400 || observed.calls != 0 {
			t.Fatalf("invalid %s accepted: status=%d calls=%d", invalid, out.Code, observed.calls)
		}
	}
	out := httptest.NewRecorder()
	server.Handler().ServeHTTP(out, partRequest(upload.UploadID, 257, md5value))
	if out.Code != 200 {
		t.Fatalf("upload status %d %s", out.Code, out.Body.String())
	}
	partCompleteAndVerify(t, store, upload.UploadID, 257, digest)
	if !observed.owned {
		t.Fatal("S3 multipart handed validated bytes to storage as a borrowed reader")
	}
}

func TestMultipartOwnedHTTPAllocation(t *testing.T) {
	const size = int64(8 << 20)
	md5value, digest := partHashes(size)
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprintf("bound=%t", bound), func(t *testing.T) {
			store, ns, path := partStore(t, bound, 128<<20)
			upload := partUpload(t, store)
			server, err := s3api.New(s3api.Config{Store: store, Auth: s3api.DevBypass})
			if err != nil {
				t.Fatal(err)
			}
			req := partRequest(upload.UploadID, size, md5value)
			out := httptest.NewRecorder()
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			server.Handler().ServeHTTP(out, req)
			runtime.ReadMemStats(&after)
			if out.Code != 200 {
				t.Fatalf("upload status %d %s", out.Code, out.Body.String())
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("8 MiB S3 UploadPart allocated %d bytes", allocated)
			if allocated > 36<<20 {
				t.Errorf("multipart upload duplicated owned input: allocated=%d budget=%d", allocated, 36<<20)
			}
			if ns != nil {
				v, e := ns.Snapshot(context.Background())
				if e != nil || v.PayloadBytes != size || v.PendingBytes != 0 {
					t.Fatalf("charge %+v %v", v, e)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			var reopened *fsstore.FilesystemStore
			if ns == nil {
				reopened, err = fsstore.NewFilesystemStore(path)
			} else {
				reopened, err = fsstore.NewFilesystemStoreWithNamespace(path, ns)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			partCompleteAndVerify(t, reopened, upload.UploadID, size, digest)
		})
	}
}

func TestMultipartOwnedRefusalsPreservePart(t *testing.T) {
	ctx := context.Background()
	store, ns, path := partStore(t, true, 4<<20)
	upload := partUpload(t, store)
	borrowed := []byte("old part")
	part, err := store.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(borrowed))
	if err != nil {
		t.Fatal(err)
	}
	borrowed[0] = 'X'
	before, err := ns.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bad := storage.NewOwnedBody([]byte("retry"))
	if _, err := store.UploadPart(ctx, upload.UploadID, 0, bad); !errors.Is(err, storage.ErrInvalidPart) || bad.Len() != 5 {
		t.Fatalf("invalid part consumed body: %v", err)
	}
	if _, err := store.UploadPart(ctx, upload.UploadID, 1, io.MultiReader(strings.NewReader("new"), partFailedReader{})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if _, err := store.UploadPart(ctx, upload.UploadID, 1, storage.NewOwnedBody(make([]byte, 3<<20))); !errors.Is(err, capacity.ErrFull) {
		t.Fatalf("quota error %v", err)
	}
	after, err := ns.Snapshot(ctx)
	if err != nil || after.PayloadBytes != before.PayloadBytes || after.PendingBytes != 0 {
		t.Fatalf("refusal charge before=%+v after=%+v %v", before, after, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := fsstore.NewFilesystemStoreWithNamespace(path, ns)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	partVerifyOld(t, reopened, upload.UploadID, part)
}

type partFailedReader struct{}

func (partFailedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func partVerifyOld(t *testing.T, store *fsstore.FilesystemStore, id string, part *storage.PartInfo) {
	t.Helper()
	ctx := context.Background()
	parts, err := store.ListParts(ctx, id)
	if err != nil || len(parts) != 1 || !storage.ETagEqual(parts[0].ETag, part.ETag) || parts[0].Size != 8 {
		t.Fatalf("refused replacement changed part: %+v %v", parts, err)
	}
	if _, err := store.CompleteMultipartUpload(ctx, id, parts); err != nil {
		t.Fatal(err)
	}
	reader, _, err := store.GetObject(ctx, "bucket", "payload")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "old part" {
		t.Fatalf("borrowed/refused part changed: %q %v", data, err)
	}
}
