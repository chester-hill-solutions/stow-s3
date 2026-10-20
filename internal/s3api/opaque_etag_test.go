package s3api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// An entity tag is opaque. It identifies a representation, so a tag that
// differs only in case names a different representation and a condition
// carrying it must not match.
//
// The routes are the ones a client can use a tag on: the two write
// preconditions, the two copy-source preconditions and the two read
// validators.
func TestAnOpaqueEntityTagIsNotFoldedByCase(t *testing.T) {
	cases := []struct {
		name, header                 string
		copy                         bool
		read                         bool
		equalStatus, differentStatus int
	}{
		{"put-match", "If-Match", false, false, 200, 412},
		{"put-none", "If-None-Match", false, false, 412, 200},
		{"copy-match", "X-Amz-Copy-Source-If-Match", true, false, 200, 412},
		{"copy-none", "X-Amz-Copy-Source-If-None-Match", true, false, 412, 200},
		{"get-match", "If-Match", false, true, 200, 412},
		{"get-none", "If-None-Match", false, true, 304, 200},
	}
	for _, tc := range cases {
		for _, changeCase := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/caseChanged=%t", tc.name, changeCase), func(t *testing.T) {
				store := storage.NewMemoryStore()
				ctx := context.Background()
				if err := store.CreateBucket(ctx, "bucket"); err != nil {
					t.Fatal(err)
				}
				meta, err := store.PutObject(ctx, "bucket", "source", strings.NewReader("old"), storage.PutOptions{})
				if err != nil {
					t.Fatal(err)
				}
				tag := meta.ETag
				want := tc.equalStatus
				if changeCase {
					tag = strings.ToUpper(tag)
					want = tc.differentStatus
					if tag == meta.ETag {
						t.Fatal("the fixture has no letters to change")
					}
				}
				server, err := s3api.New(s3api.Config{Store: store, Auth: s3api.DevBypass})
				if err != nil {
					t.Fatal(err)
				}
				method, path, body := http.MethodPut, "http://localhost/bucket/source", "replacement"
				if tc.copy {
					path, body = "http://localhost/bucket/destination", ""
				}
				if tc.read {
					method, path, body = http.MethodGet, "http://localhost/bucket/source", ""
				}
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Length", fmt.Sprint(len(body)))
				req.Header.Set(tc.header, tag)
				if tc.copy {
					req.Header.Set("X-Amz-Copy-Source", "/bucket/source")
				}
				out := httptest.NewRecorder()
				server.Handler().ServeHTTP(out, req)
				if out.Code != want {
					t.Errorf("%s with tag %q = %d, want %d", tc.name, tag, out.Code, want)
				}
				// A refused write leaves the object it refused to replace, and a
				// refused copy leaves no destination behind.
				if changeCase && want == 412 {
					assertSourceIntact(t, store, ctx, tc.copy)
				}
			})
		}
	}
}

// A refusal is only worth something if the object it refused to replace is
// still there.
func assertSourceIntact(t *testing.T, store *storage.MemoryStore, ctx context.Context, copy bool) {
	t.Helper()
	if copy {
		if _, _, err := store.GetObject(ctx, "bucket", "destination"); err == nil {
			t.Error("a refused copy left the destination behind")
		}
		return
	}
	reader, _, err := store.GetObject(ctx, "bucket", "source")
	if err != nil {
		t.Fatalf("read the source back: %v", err)
	}
	defer reader.Close()
	kept, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read the source back: %v", err)
	}
	if string(kept) != "old" {
		t.Errorf("the source now holds %q, want the bytes it had", kept)
	}
}
