package stow_test

import (
	"context"
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

func TestCustomMissingUploadS3Status(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrapped=%t", wrapped), func(t *testing.T) {
			f := newAbortPublicationFixture(t, false)
			id := customAbortUpload(t, f)
			if err := f.inner.AbortMultipartUpload(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			if wrapped {
				f.store.missing = fmt.Errorf("upload missing: %w", stow.ErrObjectNotFound)
			}
			adapter, err := stowruntime.NewStoreAdapter(f.inner)
			if err != nil {
				t.Fatal(err)
			}
			server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
			if err != nil {
				t.Fatal(err)
			}
			completion := fmt.Sprintf("<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>", storage.ETagForBytes([]byte("part")))
			cases := []struct{ name, method, query, body string }{
				{"upload-part", http.MethodPut, "&partNumber=1", "part"},
				{"complete", http.MethodPost, "", completion},
				{"abort", http.MethodDelete, "", ""},
				{"list-parts", http.MethodGet, "", ""},
			}
			for _, item := range cases {
				t.Run(item.name, func(t *testing.T) {
					request := httptest.NewRequest(item.method, "/probe/target?uploadId="+id+item.query, strings.NewReader(item.body))
					request.Header.Set("Content-Length", fmt.Sprint(len(item.body)))
					response := httptest.NewRecorder()
					server.ServeHTTP(response, request)
					if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "<Code>NoSuchUpload</Code>") {
						t.Errorf("missing upload status=%d body=%s", response.Code, response.Body.String())
					}
				})
			}
		})
	}
}
