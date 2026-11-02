package stow_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type s3RecoveryRequest struct{ name, method, query, body string }

func TestS3AbortRetryRecovery(t *testing.T) {
	completion := fmt.Sprintf("<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>", storage.ETagForBytes([]byte("part")))
	requests := []s3RecoveryRequest{
		{"abort", http.MethodDelete, "", ""},
		{"upload-part", http.MethodPut, "&partNumber=1", "part"},
		{"complete", http.MethodPost, "", completion},
		{"list-parts", http.MethodGet, "", ""},
	}
	for _, exists := range []bool{false, true} {
		for _, wrapped := range []bool{false, true} {
			for _, request := range requests {
				t.Run(fmt.Sprintf("exists=%t/wrapped=%t/%s", exists, wrapped, request.name), func(t *testing.T) { checkS3AbortRecovery(t, exists, wrapped, request) })
			}
		}
	}
}

func checkS3AbortRecovery(t *testing.T, exists, wrapped bool, request s3RecoveryRequest) {
	t.Helper()
	f, store := newCustomAbortRetryFixture(t, exists)
	id := customAbortUpload(t, f)
	f.store.abortErr = errors.New("abort acknowledgement unavailable")
	adapter, err := stowruntime.NewStoreAdapter(f.inner)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	first := httptest.NewRecorder()
	server.ServeHTTP(first, httptest.NewRequest(http.MethodDelete, "/probe/target?uploadId="+id, nil))
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first abort status=%d body=%s", first.Code, first.Body.String())
	}
	assertCustomAbortGone(t, f)
	if wrapped {
		f.store.missing = fmt.Errorf("lookup: %w", stow.ErrObjectNotFound)
	}
	retry := httptest.NewRecorder()
	req := httptest.NewRequest(request.method, "/probe/target?uploadId="+id+request.query, strings.NewReader(request.body))
	req.Header.Set("Content-Length", fmt.Sprint(len(request.body)))
	server.ServeHTTP(retry, req)
	if retry.Code != http.StatusNotFound || !strings.Contains(retry.Body.String(), "<Code>NoSuchUpload</Code>") {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	if store.attempts != 1 {
		t.Errorf("validation mutated backing uploads: abort attempts=%d", store.attempts)
	}
	assertCustomAbortGone(t, f)
	checkCustomAbortCapacity(t, f)
}
