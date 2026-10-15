package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type completionPublicationFixture struct {
	instance *Instance
	root     string
	upload   string
	part     *storage.PartInfo
	repair   func()
	version  string
}

func newCompletionPublicationFixture(t *testing.T) *completionPublicationFixture {
	t.Helper()
	f := &completionPublicationFixture{root: t.TempDir(), repair: func() {}}
	f.instance = openCompletionWorkspace(t, f.root)
	t.Cleanup(func() { f.repair(); _ = f.instance.Close() })
	upload, err := f.instance.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "retained"}})
	if err != nil {
		t.Fatal(err)
	}
	f.upload = upload.UploadID
	f.part = mustUploadPart(t, context.Background(), f.instance, f.upload, []byte("body"))
	return f
}

func openCompletionWorkspace(t *testing.T, root string) *Instance {
	t.Helper()
	store, err := workspace.New(workspace.Options{Root: root, Bucket: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1}, store, nil)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return instance
}

func TestWorkspaceCompletionPublicationFailureRetainsAccounting(t *testing.T) {
	for _, failure := range []string{"index", "cleanup"} {
		t.Run(failure, func(t *testing.T) {
			f := newCompletionPublicationFixture(t)
			f.block(t, failure)
			meta, err := completionWithoutPanic(t, f.instance, f.upload, f.part)
			if !errors.Is(err, storage.ErrMutationCommitted) || meta == nil {
				t.Fatalf("committed completion meta=%+v error=%v", meta, err)
			}
			wantETag := storage.CompletionETag([]string{f.part.ETag})
			if meta.ETag != wantETag || meta.VersionID == "" {
				t.Fatalf("committed identity=%+v, want multipart ETag %s", meta, wantETag)
			}
			f.version = meta.VersionID
			assertRetainedCompletion(t, f, wantETag)
			f.repair()
			if err := f.instance.Close(); err != nil {
				t.Fatal(err)
			}
			f.instance = openCompletionWorkspace(t, f.root)
			assertRetainedCompletion(t, f, wantETag)
			assertCompletionRetryReleasesParts(t, f, wantETag)
		})
	}
}

func TestWorkspaceCommittedCompletionCanAbortAfterRepair(t *testing.T) {
	f := newCompletionPublicationFixture(t)
	f.block(t, "index")
	meta, err := completionWithoutPanic(t, f.instance, f.upload, f.part)
	if !errors.Is(err, storage.ErrMutationCommitted) || meta == nil {
		t.Fatalf("committed completion meta=%+v error=%v", meta, err)
	}
	f.repair()
	ctx := context.Background()
	if err := f.instance.AbortMultipartUpload(ctx, f.upload); err != nil {
		t.Fatal(err)
	}
	object, err := f.instance.GetObject(ctx, "probe", "target")
	if err != nil || string(object.Data) != "body" || object.VersionID != meta.VersionID || object.ETag != meta.ETag {
		t.Fatalf("abort changed committed object: %+v %v", object, err)
	}
	if f.instance.Usage() != (Usage{Bytes: 4, Objects: 1}) || f.instance.reservedBytes != 0 {
		t.Fatalf("abort accounting: usage=%+v reserved=%d", f.instance.Usage(), f.instance.reservedBytes)
	}
	if _, err := f.instance.PutObject(ctx, "probe", "target", []byte("12345678"), PutOptions{}); err != nil {
		t.Fatalf("aborted parts still charged: %v", err)
	}
}

func (f *completionPublicationFixture) block(t *testing.T, failure string) {
	t.Helper()
	var once sync.Once
	if failure == "index" {
		index := filepath.Join(f.root, ".stow", "index.json")
		if err := os.Rename(index, index+".saved"); err != nil {
			t.Fatal(err)
		}
		f.repair = func() { once.Do(func() { _ = os.Remove(index); _ = os.Rename(index+".saved", index) }) }
		if err := os.Mkdir(index, 0700); err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Geteuid() == 0 {
		t.Skip("cleanup failure requires enforced directory permissions")
	}
	dir := filepath.Join(f.root, ".stow", "multipart", f.upload)
	f.repair = func() { once.Do(func() { _ = os.Chmod(dir, 0755) }) }
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "permission-probe")
	if err := os.WriteFile(probe, nil, 0600); err == nil {
		_ = os.Remove(probe)
		t.Skip("host does not enforce directory permissions")
	}
}

func completionWithoutPanic(t *testing.T, instance *Instance, upload string, part *storage.PartInfo) (meta *storage.ObjectMeta, err error) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			t.Errorf("completion panicked after publication: %v", value)
		}
	}()
	return instance.CompleteMultipartUpload(context.Background(), upload, []storage.PartInfo{*part})
}

func assertRetainedCompletion(t *testing.T, f *completionPublicationFixture, wantETag string) {
	t.Helper()
	ctx := context.Background()
	object, err := f.instance.GetObject(ctx, "probe", "target")
	if err != nil || string(object.Data) != "body" || object.ETag != wantETag || object.VersionID != f.version || object.ContentType != "text/plain" || object.Metadata["state"] != "retained" {
		t.Fatalf("published object=%+v error=%v", object, err)
	}
	if f.instance.Usage() != (Usage{Bytes: 4, Objects: 1}) || f.instance.reservedBytes != 4 {
		t.Fatalf("usage=%+v part bytes=%d", f.instance.Usage(), f.instance.reservedBytes)
	}
	parts, err := f.instance.ListParts(ctx, f.upload)
	if err != nil || len(parts) != 1 || parts[0].ETag != f.part.ETag {
		t.Fatalf("retained parts=%+v error=%v", parts, err)
	}
	if _, err := f.instance.PutObject(ctx, "probe", "target", []byte("12345"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("retained part budget escaped quota: %v", err)
	}
	if _, err := f.instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("retained upload released its session slot: %v", err)
	}
}

func assertCompletionRetryReleasesParts(t *testing.T, f *completionPublicationFixture, wantETag string) {
	t.Helper()
	ctx := context.Background()
	meta, err := f.instance.CompleteMultipartUpload(ctx, f.upload, []storage.PartInfo{*f.part})
	if err != nil || meta.ETag != wantETag || f.instance.reservedBytes != 0 {
		t.Fatalf("completion retry=%+v reserved=%d error=%v", meta, f.instance.reservedBytes, err)
	}
	if _, err := f.instance.GetMultipartUpload(ctx, f.upload); !errors.Is(err, storage.ErrUploadNotFound) {
		t.Fatalf("completed upload still present: %v", err)
	}
	if _, err := f.instance.PutObject(ctx, "probe", "target", []byte("12345678"), PutOptions{}); err != nil {
		t.Fatalf("completed parts still charged: %v", err)
	}
	mustCreateUpload(t, ctx, f.instance, "probe", "target")
}

type committedCompletionStore struct {
	*storage.MemoryStore
	completed bool
	lookupErr error
}

func (s *committedCompletionStore) CompleteMultipartUpload(ctx context.Context, upload string, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	meta, err := s.MemoryStore.CompleteMultipartUpload(ctx, upload, parts)
	if err == nil {
		s.completed = true
		err = storage.CommittedError(errors.New("after successful local completion"))
	}
	return meta, err
}

func (s *committedCompletionStore) GetMultipartUpload(ctx context.Context, upload string) (*storage.MultipartUpload, error) {
	if s.completed && s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.MemoryStore.GetMultipartUpload(ctx, upload)
}

func TestCommittedCompletionRetainsReservationOnUnknownUpload(t *testing.T) {
	ctx := context.Background()
	lookupErr := errors.New("upload lookup unavailable")
	store := &committedCompletionStore{MemoryStore: storage.NewMemoryStore(), lookupErr: lookupErr}
	instance, err := OpenWithStore(Options{MaxBytes: 8, MaxObjects: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	mustCreateBucket(t, ctx, instance, "probe")
	upload := mustCreateUpload(t, ctx, instance, "probe", "target")
	part := mustUploadPart(t, ctx, instance, upload, []byte("body"))
	meta, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part})
	if !errors.Is(err, storage.ErrMutationCommitted) || !errors.Is(err, lookupErr) || meta == nil {
		t.Fatalf("unknown cleanup lost errors or identity: %+v %v", meta, err)
	}
	if instance.Usage() != (Usage{Bytes: 4, Objects: 1}) || instance.reservedBytes != 4 {
		t.Fatalf("unknown cleanup released quota: usage=%+v reserved=%d", instance.Usage(), instance.reservedBytes)
	}
}

func TestCommittedCompletionWithRemovedUploadReleasesParts(t *testing.T) {
	ctx := context.Background()
	store := &committedCompletionStore{MemoryStore: storage.NewMemoryStore()}
	instance, err := OpenWithStore(Options{MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	mustCreateBucket(t, ctx, instance, "probe")
	upload := mustCreateUpload(t, ctx, instance, "probe", "target")
	part := mustUploadPart(t, ctx, instance, upload, []byte("body"))
	meta, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part})
	if !errors.Is(err, storage.ErrMutationCommitted) || meta == nil || instance.Usage() != (Usage{Bytes: 4, Objects: 1}) || instance.reservedBytes != 0 {
		t.Fatalf("completed meta=%+v usage=%+v reserved=%d error=%v", meta, instance.Usage(), instance.reservedBytes, err)
	}
	mustCreateUpload(t, ctx, instance, "probe", "target")
}

func BenchmarkWorkspaceMultipartCompletion(b *testing.B) {
	ctx := context.Background()
	store, err := workspace.New(workspace.Options{Root: b.TempDir(), Bucket: "probe"})
	if err != nil {
		b.Fatal(err)
	}
	instance, err := OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 128 << 10, MaxObjects: 1}, store, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = instance.Close() })
	body := bytes.Repeat([]byte("x"), 4096)
	b.ReportAllocs()
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		upload, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
		if err != nil {
			b.Fatal(err)
		}
		part, err := instance.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(body))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := instance.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
			b.Fatal(err)
		}
	}
}
