package s3api_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// Embedding only Store models a custom backend without optional conditional COPY.
type basicCopyStore struct{ storage.Store }

type copyAdapterCase struct {
	name, header, value string
	status              int
	existing            bool
}

func TestCopyAdapterPreservesSourceConditions(t *testing.T) {
	for _, profile := range []string{"basic", "conditional"} {
		t.Run(profile, func(t *testing.T) {
			server, memory, source := copyAdapterFixture(t, profile)
			past := source.LastModified.Add(-time.Hour).Format(http.TimeFormat)
			future := source.LastModified.Add(time.Hour).Format(http.TimeFormat)
			cases := []copyAdapterCase{
				{"unconditional", "", "", http.StatusOK, false},
				{"match-pass", "If-Match", source.ETag, http.StatusOK, false},
				{"match-fail", "If-Match", "\"different\"", http.StatusPreconditionFailed, false},
				{"none-match-pass", "If-None-Match", "\"different\"", http.StatusOK, false},
				{"none-match-fail", "If-None-Match", source.ETag, http.StatusPreconditionFailed, false},
				{"modified-pass", "If-Modified-Since", past, http.StatusOK, false},
				{"modified-fail", "If-Modified-Since", future, http.StatusPreconditionFailed, false},
				{"unmodified-pass", "If-Unmodified-Since", future, http.StatusOK, false},
				{"unmodified-fail", "If-Unmodified-Since", past, http.StatusPreconditionFailed, false},
			}
			for _, directive := range []string{"COPY", "REPLACE"} {
				for _, tc := range cases {
					for _, existing := range []bool{false, true} {
						tc.existing = existing
						name := fmt.Sprintf("%s/%s/existing=%t", directive, tc.name, existing)
						t.Run(name, func(t *testing.T) {
							checkCopyAdapterCase(t, server, memory, directive, tc)
						})
					}
				}
			}
		})
	}
}

func copyAdapterFixture(t *testing.T, profile string) (*s3api.Server, *storage.MemoryStore, *storage.ObjectMeta) {
	t.Helper()
	ctx := context.Background()
	memory := storage.NewMemoryStore()
	if err := memory.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	source, err := memory.PutObject(ctx, "bucket", "source", strings.NewReader("captured source"), storage.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"generation": "one"}})
	if err != nil {
		t.Fatal(err)
	}
	var local storage.Store = &basicCopyStore{Store: memory}
	if profile == "conditional" {
		local = memory
	}
	adapter := runthrough.New(runthrough.Config{}, local, nil)
	t.Cleanup(func() { _ = adapter.Close() })
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	return server, memory, source
}

func checkCopyAdapterCase(t *testing.T, server *s3api.Server, store storage.Store, directive string, tc copyAdapterCase) {
	t.Helper()
	ctx := context.Background()
	key := strings.ReplaceAll(t.Name(), "/", "-")
	if tc.existing {
		if _, err := store.PutObject(ctx, "bucket", key, strings.NewReader("keep destination"), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(http.MethodPut, "/bucket/"+key, nil)
	request.Header.Set("X-Amz-Copy-Source", "/bucket/source")
	request.Header.Set("X-Amz-Metadata-Directive", directive)
	if tc.header != "" {
		request.Header.Set("X-Amz-Copy-Source-"+tc.header, tc.value)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != tc.status {
		t.Errorf("COPY status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
	}
	if tc.status != http.StatusOK && !tc.existing {
		if _, err := store.HeadObject(ctx, "bucket", key); !errors.Is(err, storage.ErrObjectNotFound) {
			t.Fatalf("refused COPY created destination: %v", err)
		}
		return
	}
	assertCopyAdapterDestination(t, store, key, directive, tc.status)
}

func assertCopyAdapterDestination(t *testing.T, store storage.Store, key, directive string, status int) {
	t.Helper()
	rc, meta, err := store.GetObject(context.Background(), "bucket", key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	want := "captured source"
	if status != http.StatusOK {
		want = "keep destination"
	}
	if string(body) != want {
		t.Errorf("destination = %q, want %q", body, want)
	}
	if status == http.StatusOK && directive == "COPY" && (meta.ContentType != "text/plain" || meta.Metadata["generation"] != "one") {
		t.Errorf("COPY lost source metadata: %+v", meta)
	}
}
