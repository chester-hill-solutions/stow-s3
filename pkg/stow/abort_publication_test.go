package stow_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type abortPublicationStore struct {
	*initiationPublicationStore
	abortErr    error
	refuseAbort bool
	abortCalls  int
}

func (s *abortPublicationStore) AbortMultipartUpload(ctx context.Context, id string) error {
	s.abortCalls++
	if s.refuseAbort {
		return s.abortErr
	}
	if err := s.initiationPublicationStore.AbortMultipartUpload(ctx, id); err != nil {
		return err
	}
	return s.abortErr
}

type abortFixture struct {
	store  *abortPublicationStore
	outer  *stow.Runtime
	inner  *stowruntime.Instance
	exists bool
}

func newAbortPublicationFixture(t testing.TB, exists bool) *abortFixture {
	t.Helper()
	store := &abortPublicationStore{initiationPublicationStore: &initiationPublicationStore{partPublicationStore: &partPublicationStore{singlePartPublicationStore: newSinglePartPublicationStore()}}}
	outer, err := stow.Open(stow.Options{Store: store, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outer.Close() })
	if err := outer.CreateBucket(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	if exists {
		if _, err := outer.PutObject(context.Background(), "probe", "target", []byte("old"), stow.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return &abortFixture{store: store, outer: outer, inner: embedded.UnwrapInstance(outer), exists: exists}
}

func customAbortUpload(t testing.TB, f *abortFixture) string {
	t.Helper()
	upload, err := f.inner.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "abort"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.inner.UploadPart(context.Background(), upload.UploadID, 1, strings.NewReader("part")); err != nil {
		t.Fatal(err)
	}
	return upload.UploadID
}

func TestCustomAbortPublication(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, outcome := range []string{"success", "committed", "committed-missing", "refused"} {
			t.Run(fmt.Sprintf("exists=%t/%s", exists, outcome), func(t *testing.T) { checkCustomAbort(t, exists, outcome) })
		}
	}
}

func checkCustomAbort(t *testing.T, exists bool, outcome string) {
	t.Helper()
	f := newAbortPublicationFixture(t, exists)
	id := customAbortUpload(t, f)
	switch outcome {
	case "committed":
		f.store.abortErr = errors.Join(stow.ErrMutationCommitted, errors.New("after abort"))
	case "committed-missing":
		f.store.abortErr = errors.Join(stow.ErrMutationCommitted, stow.ErrObjectNotFound)
	case "refused":
		f.store.abortErr, f.store.refuseAbort = stow.ErrPreconditionFailed, true
	}
	if err := f.inner.AbortMultipartUpload(context.Background(), id); !errors.Is(err, f.store.abortErr) || f.store.abortCalls != 1 {
		t.Fatalf("abort error=%v provider calls=%d", err, f.store.abortCalls)
	}
	if f.store.refuseAbort {
		assertCustomAbortRetained(t, f, id)
		f.store.refuseAbort, f.store.abortErr = false, nil
		if err := f.inner.AbortMultipartUpload(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	assertCustomAbortGone(t, f)
	calls := f.store.abortCalls
	if err := f.inner.AbortMultipartUpload(context.Background(), id); !errors.Is(err, storage.ErrUploadNotFound) || f.store.abortCalls != calls {
		t.Errorf("repeat abort error=%v provider calls=%d", err, f.store.abortCalls-calls)
	}
	checkCustomAbortCapacity(t, f)
}

func assertCustomAbortRetained(t *testing.T, f *abortFixture, id string) {
	t.Helper()
	parts, err := f.store.ListParts(context.Background(), id)
	if err != nil || len(parts) != 1 || parts[0].Size != 4 || string(f.store.body) != "part" {
		t.Fatalf("refusal changed part: %+v body=%q error=%v", parts, f.store.body, err)
	}
	if _, err := f.outer.PutObject(context.Background(), "probe", "target", []byte("12345"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("refusal released retained bytes: %v", err)
	}
	if _, err := f.inner.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{}); !errors.Is(err, stowruntime.ErrQuotaExceeded) {
		t.Fatalf("refusal released upload slot: %v", err)
	}
}

func assertCustomAbortGone(t *testing.T, f *abortFixture) {
	t.Helper()
	uploads, err := f.store.ListMultipartUploads(context.Background(), "probe")
	if err != nil || len(uploads) != 0 || len(f.store.body) != 0 {
		t.Fatalf("abort retained data: uploads=%+v body=%q error=%v", uploads, f.store.body, err)
	}
	want := stow.Usage{}
	if f.exists {
		want = stow.Usage{Bytes: 3, Objects: 1}
		object, err := f.outer.GetObject(context.Background(), "probe", "target")
		if err != nil || string(object.Data) != "old" {
			t.Fatalf("abort changed existing object: %+v %v", object, err)
		}
	}
	if f.outer.Usage() != want {
		t.Fatalf("abort object usage=%+v want %+v", f.outer.Usage(), want)
	}
}

func checkCustomAbortCapacity(t *testing.T, f *abortFixture) {
	t.Helper()
	ctx := context.Background()
	if err := f.outer.DeleteObject(ctx, "probe", "target"); err != nil && !errors.Is(err, stow.ErrObjectNotFound) {
		t.Fatal(err)
	}
	if _, err := f.outer.PutObject(ctx, "probe", "other", []byte("12345678"), stow.PutOptions{}); err != nil {
		t.Errorf("abort stranded byte/object capacity: %v", err)
	}
	f.store.abortErr = nil
	next, err := f.inner.CreateMultipartUpload(ctx, "probe", "other", storage.MultipartOptions{})
	if err != nil {
		t.Errorf("abort stranded upload slot: %v", err)
		return
	}
	if err := f.inner.AbortMultipartUpload(ctx, next.UploadID); err != nil {
		t.Fatal(err)
	}
}

func TestCustomCommittedAbortS3Error(t *testing.T) {
	f := newAbortPublicationFixture(t, false)
	id := customAbortUpload(t, f)
	f.store.abortErr = errors.Join(stow.ErrMutationCommitted, errors.New("after abort"))
	adapter, err := stowruntime.NewStoreAdapter(f.inner)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/probe/target?uploadId="+id, nil))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "<Code>InternalError</Code>") {
		t.Fatalf("S3 status=%d body=%s", response.Code, response.Body.String())
	}
	assertCustomAbortGone(t, f)
	checkCustomAbortCapacity(t, f)
}

func BenchmarkCustomAbortSuccessLifecycle(b *testing.B) {
	f := newAbortPublicationFixture(b, false)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		id := customAbortUpload(b, f)
		if err := f.inner.AbortMultipartUpload(context.Background(), id); err != nil {
			b.Fatal(err)
		}
	}
}
