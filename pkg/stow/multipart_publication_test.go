package stow_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func assertCustomMultipartMetadata(t *testing.T, got, want *storage.ObjectMeta) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("completion identity=%+v, want %+v", got, want)
	}
}

func TestCustomMultipartCompletionPublication(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		for _, outcome := range []string{"success", "retained", "removed", "removed-wrapped", "unknown", "unknown-missing", "refused"} {
			t.Run(fmt.Sprintf("overwrite=%t/%s", overwrite, outcome), func(t *testing.T) {
				checkCustomMultipartCompletion(t, overwrite, outcome)
			})
		}
	}
}

func checkCustomMultipartCompletion(t *testing.T, overwrite bool, outcome string) {
	t.Helper()
	f := newCustomMultipartFixture(t, overwrite)
	cause := errors.New("after multipart publication")
	if outcome != "success" {
		f.store.failure = errors.Join(stow.ErrMutationCommitted, cause)
	}
	switch outcome {
	case "retained", "unknown", "unknown-missing":
		f.store.remove = false
	case "removed-wrapped":
		f.store.missing = fmt.Errorf("upload missing: %w", stow.ErrObjectNotFound)
	case "refused":
		f.store.refuse, f.store.failure = true, stow.ErrPreconditionFailed
	}
	if strings.HasPrefix(outcome, "unknown") {
		f.store.lookupErr = errors.New("upload lookup unavailable")
		if outcome == "unknown-missing" {
			f.store.lookupErr = errors.Join(f.store.lookupErr, stow.ErrObjectNotFound, stow.ErrMutationCommitted)
		}
	}
	meta, err := completeCustomMultipart(t, f)
	if outcome == "refused" {
		assertCustomMultipartRefusal(t, f, meta, err)
		return
	}
	assertCustomMultipartOutcome(t, f, err)
	assertCustomMultipartIdentity(t, f, meta)
	if f.store.remove {
		assertCustomMultipartReleased(t, f)
		return
	}
	assertCustomMultipartRetained(t, f)
	f.store.failure, f.store.lookupErr, f.store.remove = nil, nil, true
	if strings.HasPrefix(outcome, "unknown") {
		if err := f.inner.AbortMultipartUpload(context.Background(), f.upload.UploadID); err != nil {
			t.Fatal(err)
		}
		assertCustomMultipartIdentity(t, f, meta)
	} else {
		retried, err := completeCustomMultipart(t, f)
		if err != nil {
			t.Fatal(err)
		}
		assertCustomMultipartIdentity(t, f, retried)
	}
	assertCustomMultipartReleased(t, f)
}

func assertCustomMultipartOutcome(t *testing.T, f *customMultipartFixture, err error) {
	t.Helper()
	if !errors.Is(err, f.store.failure) {
		t.Fatalf("completion lost error: %v, want %v", err, f.store.failure)
	}
	wantGets := 0
	if f.store.failure != nil {
		wantGets = 1
	}
	if f.store.getCalls != wantGets {
		t.Fatalf("completion lookup calls=%d, want %d", f.store.getCalls, wantGets)
	}
	if f.store.lookupErr != nil && !errors.Is(err, f.store.lookupErr) {
		t.Fatalf("unknown cleanup lost lookup error: %v", err)
	}
}

func assertCustomMultipartRetained(t *testing.T, f *customMultipartFixture) {
	t.Helper()
	ctx := context.Background()
	parts, err := f.store.ListParts(ctx, f.upload.UploadID)
	if err != nil || len(parts) != 1 || parts[0].Size != 4 {
		t.Fatalf("retained parts=%+v error=%v", parts, err)
	}
	if _, err := f.outer.PutObject(ctx, "probe", "target", []byte("12345"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("retained parts escaped byte quota: %v", err)
	}
	if _, err := f.inner.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{}); !errors.Is(err, stowruntime.ErrQuotaExceeded) {
		t.Fatalf("retained upload released its session slot: %v", err)
	}
}

func assertCustomMultipartReleased(t *testing.T, f *customMultipartFixture) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.inner.GetMultipartUpload(ctx, f.upload.UploadID); !errors.Is(err, storage.ErrUploadNotFound) {
		t.Errorf("removed upload lookup=%v, want ErrUploadNotFound", err)
	}
	if _, err := f.outer.PutObject(ctx, "probe", "target", []byte("12345678"), stow.PutOptions{}); err != nil {
		t.Fatalf("removed parts still consume byte quota: %v", err)
	}
	if _, err := f.inner.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{}); err != nil {
		t.Fatalf("removed upload still consumes a session: %v", err)
	}
}

func assertCustomMultipartRefusal(t *testing.T, f *customMultipartFixture, meta *storage.ObjectMeta, err error) {
	t.Helper()
	if meta != nil || !errors.Is(err, stow.ErrPreconditionFailed) || f.store.getCalls != 0 {
		t.Fatalf("refusal meta=%+v error=%v lookups=%d", meta, err, f.store.getCalls)
	}
	want := stow.Usage{}
	object, readErr := f.store.GetObject(context.Background(), "probe", "target")
	if f.overwrite {
		want = stow.Usage{Bytes: 3, Objects: 1}
		if readErr != nil || string(object.Data) != "old" {
			t.Fatalf("refusal changed object: %+v %v", object, readErr)
		}
	} else if !errors.Is(readErr, stow.ErrObjectNotFound) {
		t.Fatalf("refusal published object: %+v %v", object, readErr)
	}
	if f.outer.Usage() != want {
		t.Fatalf("refusal usage=%+v, want %+v", f.outer.Usage(), want)
	}
	f.store.refuse, f.store.failure = false, nil
	completed, completeErr := completeCustomMultipart(t, f)
	if completeErr != nil {
		t.Fatal(completeErr)
	}
	assertCustomMultipartIdentity(t, f, completed)
	assertCustomMultipartReleased(t, f)
}

func TestCustomMultipartCommittedCompletionS3Error(t *testing.T) {
	f := newCustomMultipartFixture(t, false)
	f.store.remove = false
	f.store.failure = errors.Join(stow.ErrMutationCommitted, errors.New("after multipart publication"))
	adapter, err := stowruntime.NewStoreAdapter(f.inner)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>", f.part.ETag)
	request := httptest.NewRequest(http.MethodPost, "/probe/target?uploadId="+f.upload.UploadID, strings.NewReader(body))
	request.Header.Set("Content-Length", fmt.Sprint(len(body)))
	response := httptest.NewRecorder()
	serveCustomMultipartWithoutPanic(t, server, request, response)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "<Code>InternalError</Code>") {
		t.Fatalf("S3 completion status=%d body=%s", response.Code, response.Body.String())
	}
	if f.outer.Usage() != (stow.Usage{Bytes: 4, Objects: 1}) {
		t.Fatalf("S3 completion usage=%+v", f.outer.Usage())
	}
	assertCustomMultipartRetained(t, f)
}

func serveCustomMultipartWithoutPanic(t *testing.T, server *s3api.Server, request *http.Request, response *httptest.ResponseRecorder) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			t.Errorf("S3 multipart completion panicked: %v", value)
		}
	}()
	server.ServeHTTP(response, request)
}

func BenchmarkCustomMultipartCompletionLifecycle(b *testing.B) {
	ctx := context.Background()
	store := newSinglePartPublicationStore()
	outer, err := stow.Open(stow.Options{Store: store, MaxBytes: 128 << 10, MaxObjects: 1})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = outer.Close() })
	if err := outer.CreateBucket(ctx, "probe"); err != nil {
		b.Fatal(err)
	}
	inner := embedded.UnwrapInstance(outer)
	body := bytes.Repeat([]byte("x"), 4096)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		upload, err := inner.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "completed"}})
		if err != nil {
			b.Fatal(err)
		}
		part, err := inner.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(body))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := inner.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
			b.Fatal(err)
		}
	}
}
