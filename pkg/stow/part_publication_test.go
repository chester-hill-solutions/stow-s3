package stow_test

import (
	"bytes"
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

type partPublicationStore struct {
	*singlePartPublicationStore
	partErr    error
	refusePart bool
	partCalls  int
}

func (s *partPublicationStore) UploadPart(ctx context.Context, id string, number int, data []byte) (stow.Part, error) {
	s.partCalls++
	if s.refusePart {
		return stow.Part{}, s.partErr
	}
	part, err := s.singlePartPublicationStore.UploadPart(ctx, id, number, data)
	if err != nil {
		return part, err
	}
	s.multipartStore.mu.Lock()
	s.parts[id] = []stow.Part{part}
	s.multipartStore.mu.Unlock()
	return part, s.partErr
}

func TestCustomPartPublication(t *testing.T) {
	for _, oldSize := range []int{0, 2, 6} {
		for _, outcome := range []string{"success", "committed", "committed-missing", "refused"} {
			t.Run(fmt.Sprintf("old=%d/%s", oldSize, outcome), func(t *testing.T) { checkCustomPartPublication(t, oldSize, outcome) })
		}
	}
}

func checkCustomPartPublication(t *testing.T, oldSize int, outcome string) {
	t.Helper()
	ctx := context.Background()
	store, outer := openPartPublicationRuntime(t, 8)
	inner := embedded.UnwrapInstance(outer)
	upload, err := inner.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "part"}})
	if err != nil {
		t.Fatal(err)
	}
	if oldSize > 0 {
		if _, err := inner.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(bytes.Repeat([]byte("o"), oldSize))); err != nil {
			t.Fatal(err)
		}
	}
	configurePartFailure(store, outcome)
	calls := store.partCalls
	part, err := inner.UploadPart(ctx, upload.UploadID, 1, storage.NewOwnedBody([]byte("part")))
	if !errors.Is(err, store.partErr) || store.partCalls != calls+1 {
		t.Fatalf("part=%+v error=%v calls=%d", part, err, store.partCalls-calls)
	}
	if store.refusePart {
		if part != nil || len(store.body) != oldSize {
			t.Fatalf("refusal changed part: %+v body=%q", part, store.body)
		}
		store.partErr, store.refusePart = nil, false
		part, err = inner.UploadPart(ctx, upload.UploadID, 1, storage.NewOwnedBody([]byte("part")))
		if err != nil {
			t.Fatal(err)
		}
	}
	assertCustomPublishedPart(t, store, part)
	if outer.Usage() != (stow.Usage{}) {
		t.Errorf("part changed object usage: %+v", outer.Usage())
	}
	if _, err := outer.PutObject(ctx, "probe", "target", []byte("12345"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Errorf("stored part escaped quota: %v", err)
	}
	if part == nil {
		return
	}
	store.partErr = nil
	completeCustomPublishedPart(t, outer, upload.UploadID, part)
}

func configurePartFailure(store *partPublicationStore, outcome string) {
	switch outcome {
	case "committed":
		store.partErr = errors.Join(stow.ErrMutationCommitted, errors.New("after stored part"))
	case "committed-missing":
		store.partErr = errors.Join(stow.ErrMutationCommitted, stow.ErrObjectNotFound)
	case "refused":
		store.partErr, store.refusePart = stow.ErrPreconditionFailed, true
	}
}

func completeCustomPublishedPart(t *testing.T, outer *stow.Runtime, upload string, part *storage.PartInfo) {
	t.Helper()
	ctx := context.Background()
	inner := embedded.UnwrapInstance(outer)
	completed, err := inner.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part})
	if err != nil {
		t.Fatal(err)
	}
	object, err := outer.GetObject(ctx, "probe", "target")
	if err != nil || string(object.Data) != "part" || object.Metadata["state"] != "part" || object.ContentType != "text/plain" || completed.ETag != part.ETag {
		t.Fatalf("completion object=%+v metadata=%+v error=%v", object, completed, err)
	}
	if outer.Usage() != (stow.Usage{Bytes: 4, Objects: 1}) {
		t.Fatalf("completion usage=%+v", outer.Usage())
	}
	if _, err := outer.PutObject(ctx, "probe", "target", []byte("12345678"), stow.PutOptions{}); err != nil {
		t.Fatalf("completion retained phantom part quota: %v", err)
	}
}

func openPartPublicationRuntime(t testing.TB, maxBytes int64) (*partPublicationStore, *stow.Runtime) {
	t.Helper()
	store := &partPublicationStore{singlePartPublicationStore: newSinglePartPublicationStore()}
	outer, err := stow.Open(stow.Options{Store: store, MaxBytes: maxBytes, MaxObjects: 1, MaxMultipartUploads: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outer.Close() })
	if err := outer.CreateBucket(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	return store, outer
}

func assertCustomPublishedPart(t *testing.T, store *partPublicationStore, part *storage.PartInfo) {
	t.Helper()
	if part == nil {
		t.Error("committed part identity discarded")
		return
	}
	parts, err := store.ListParts(context.Background(), "upload-1")
	if err != nil || len(parts) != 1 || part.PartNumber != parts[0].PartNumber || part.ETag != parts[0].ETag || part.Size != parts[0].Size || string(store.body) != "part" {
		t.Fatalf("part=%+v actual=%+v body=%q error=%v", part, parts, store.body, err)
	}
}

func TestCustomCommittedPartS3Error(t *testing.T) {
	store, outer := openPartPublicationRuntime(t, 4)
	inner := embedded.UnwrapInstance(outer)
	upload, err := inner.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := stowruntime.NewStoreAdapter(inner)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	store.partErr = errors.Join(stow.ErrMutationCommitted, errors.New("after stored part"))
	request := httptest.NewRequest(http.MethodPut, "/probe/target?uploadId="+upload.UploadID+"&partNumber=1", strings.NewReader("part"))
	request.Header.Set("Content-Length", "4")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "<Code>InternalError</Code>") {
		t.Fatalf("S3 status=%d body=%s", response.Code, response.Body.String())
	}
	if string(store.body) != "part" {
		t.Fatal("S3 part was not stored")
	}
	if _, err := outer.PutObject(context.Background(), "probe", "target", []byte("body"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("S3 committed part escaped quota: %v", err)
	}
}

func BenchmarkCustomPartPublication(b *testing.B) {
	for _, outcome := range []string{"success", "committed"} {
		b.Run(outcome, func(b *testing.B) {
			store, outer := openPartPublicationRuntime(b, 128<<10)
			inner := embedded.UnwrapInstance(outer)
			upload, err := inner.CreateMultipartUpload(context.Background(), "probe", "target", storage.MultipartOptions{})
			if err != nil {
				b.Fatal(err)
			}
			if outcome == "committed" {
				store.partErr = errors.Join(stow.ErrMutationCommitted, errors.New("after stored part"))
			}
			body := bytes.Repeat([]byte("x"), 4096)
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				_, err := inner.UploadPart(context.Background(), upload.UploadID, 1, bytes.NewReader(body))
				if !errors.Is(err, store.partErr) {
					b.Fatal(err)
				}
			}
		})
	}
}
