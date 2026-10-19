package stow_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestCustomInitiationPublication(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, outcome := range []string{"success", "committed", "committed-missing", "refused"} {
			for _, recovery := range []string{"abort", "complete"} {
				t.Run(fmt.Sprintf("exists=%t/%s/%s", exists, outcome, recovery), func(t *testing.T) { checkCustomInitiation(t, exists, outcome, recovery) })
			}
		}
	}
}

func checkCustomInitiation(t *testing.T, exists bool, outcome, recovery string) {
	t.Helper()
	f := newInitiationFixture(t, exists)
	switch outcome {
	case "committed":
		f.store.initiationErr = errors.Join(stow.ErrMutationCommitted, errors.New("after upload initiation"))
	case "committed-missing":
		f.store.initiationErr = errors.Join(stow.ErrMutationCommitted, stow.ErrObjectNotFound)
	case "refused":
		f.store.initiationErr, f.store.refuseInitiation = stow.ErrPreconditionFailed, true
	}
	opts := storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "init"}}
	upload, err := f.inner.CreateMultipartUpload(context.Background(), "probe", "target", opts)
	if !errors.Is(err, f.store.initiationErr) || f.store.initiationCalls != 1 {
		t.Fatalf("upload=%+v error=%v calls=%d", upload, err, f.store.initiationCalls)
	}
	if f.store.refuseInitiation {
		assertCustomInitiationRefused(t, f, upload)
		f.store.initiationErr, f.store.refuseInitiation = nil, false
		upload, err = f.inner.CreateMultipartUpload(context.Background(), "probe", "target", opts)
		if err != nil {
			t.Fatal(err)
		}
	}
	assertCustomInitiationIdentity(t, f, upload)
	opts.Metadata["state"] = "input"
	assertCustomInitiationLimits(t, f)
	if upload == nil {
		return
	}
	f.store.initiationErr = nil
	recoverCustomInitiation(t, f, upload.UploadID, recovery)
}

func assertCustomInitiationRefused(t *testing.T, f *initiationFixture, upload *storage.MultipartUpload) {
	t.Helper()
	actual, err := f.store.ListMultipartUploads(context.Background(), "probe")
	if upload != nil || err != nil || len(actual) != 0 {
		t.Fatalf("refusal upload=%+v actual=%+v error=%v", upload, actual, err)
	}
	assertInitiationObjectUsage(t, f)
}

func assertCustomInitiationIdentity(t *testing.T, f *initiationFixture, upload *storage.MultipartUpload) {
	t.Helper()
	u := f.store.published
	want := &storage.MultipartUpload{UploadID: u.UploadID, Bucket: u.Bucket, Key: u.Key, Initiated: u.Initiated, Options: storage.MultipartOptions{ContentType: u.ContentType, Metadata: u.Metadata}}
	if !reflect.DeepEqual(upload, want) {
		t.Errorf("committed upload=%+v want %+v", upload, want)
	}
	if upload == nil {
		return
	}
	upload.Options.Metadata["state"] = "caller"
	actual, err := f.store.GetMultipartUpload(context.Background(), upload.UploadID)
	if err != nil || actual.Metadata["state"] != "init" {
		t.Fatalf("returned metadata aliases provider: %+v %v", actual, err)
	}
	upload.Options.Metadata["state"] = "init"
}

func assertInitiationObjectUsage(t *testing.T, f *initiationFixture) {
	t.Helper()
	want := stow.Usage{}
	if f.exists {
		want = stow.Usage{Bytes: 3, Objects: 1}
		object, err := f.outer.GetObject(context.Background(), "probe", "target")
		if err != nil || string(object.Data) != "old" {
			t.Fatalf("initiation changed existing object: %+v %v", object, err)
		}
	}
	if f.outer.Usage() != want {
		t.Errorf("initiation object usage=%+v want %+v", f.outer.Usage(), want)
	}
}

func assertCustomInitiationLimits(t *testing.T, f *initiationFixture) {
	t.Helper()
	assertInitiationObjectUsage(t, f)
	calls := f.store.initiationCalls
	if _, err := f.inner.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{}); !errors.Is(err, stowruntime.ErrQuotaExceeded) || f.store.initiationCalls != calls {
		t.Errorf("session quota error=%v provider calls=%d", err, f.store.initiationCalls-calls)
	}
	if _, err := f.outer.PutObject(context.Background(), "probe", "other", []byte("x"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Errorf("initiation object slot escaped quota: %v", err)
	}
}

func recoverCustomInitiation(t *testing.T, f *initiationFixture, upload, recovery string) {
	t.Helper()
	ctx := context.Background()
	if recovery == "abort" {
		if err := f.inner.AbortMultipartUpload(ctx, upload); err != nil {
			t.Fatal(err)
		}
		assertInitiationObjectUsage(t, f)
	} else {
		part, err := f.inner.UploadPart(ctx, upload, 1, strings.NewReader("part"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.inner.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part}); err != nil {
			t.Fatal(err)
		}
		object, err := f.outer.GetObject(ctx, "probe", "target")
		if err != nil || string(object.Data) != "part" || object.ContentType != "text/plain" || object.Metadata["state"] != "init" || f.outer.Usage() != (stow.Usage{Bytes: 4, Objects: 1}) {
			t.Fatalf("completed object=%+v error=%v usage=%+v", object, err, f.outer.Usage())
		}
	}
	if err := f.outer.DeleteObject(ctx, "probe", "target"); err != nil && !errors.Is(err, stow.ErrObjectNotFound) {
		t.Fatal(err)
	}
	if _, err := f.outer.PutObject(ctx, "probe", "other", []byte("12345678"), stow.PutOptions{}); err != nil {
		t.Fatalf("cleanup left reservations: %v", err)
	}
	next, err := f.inner.CreateMultipartUpload(ctx, "probe", "other", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("cleanup left session quota: %v", err)
	}
	if err := f.inner.AbortMultipartUpload(ctx, next.UploadID); err != nil {
		t.Fatal(err)
	}
}

func TestCustomCommittedInitiationS3Error(t *testing.T) {
	f := newInitiationFixture(t, false)
	f.store.initiationErr = errors.Join(stow.ErrMutationCommitted, errors.New("after upload initiation"))
	adapter, err := stowruntime.NewStoreAdapter(f.inner)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/probe/target?uploads", nil)
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "<Code>InternalError</Code>") {
		t.Fatalf("S3 status=%d body=%s", response.Code, response.Body.String())
	}
	actual, err := f.store.ListMultipartUploads(context.Background(), "probe")
	if err != nil || len(actual) != 1 {
		t.Fatalf("S3 initiation stored uploads=%+v error=%v", actual, err)
	}
	assertCustomInitiationLimits(t, f)
	if err := f.inner.AbortMultipartUpload(context.Background(), actual[0].UploadID); err != nil {
		t.Fatalf("S3 failed initiation cannot be recovered: %v", err)
	}
}

func BenchmarkCustomInitiationLifecycle(b *testing.B) {
	for _, outcome := range []string{"success", "committed-recovery"} {
		b.Run(outcome, func(b *testing.B) {
			f := newInitiationFixture(b, false)
			if outcome == "committed-recovery" {
				f.store.initiationErr = errors.Join(stow.ErrMutationCommitted, errors.New("after upload initiation"))
			}
			opts := storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "init"}}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				_, err := f.inner.CreateMultipartUpload(context.Background(), "probe", "target", opts)
				if !errors.Is(err, f.store.initiationErr) {
					b.Fatal(err)
				}
				err = f.inner.AbortMultipartUpload(context.Background(), f.store.published.UploadID)
				if errors.Is(err, storage.ErrUploadNotFound) {
					err = f.store.AbortMultipartUpload(context.Background(), f.store.published.UploadID)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
