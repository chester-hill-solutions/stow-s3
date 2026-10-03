package s3api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestReadConditionPrecedenceAndNotModifiedCORS(t *testing.T) {
	srv, store := regressionServer(t)
	meta, err := store.PutObject(context.Background(), "bucket", "key", strings.NewReader("body"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before := meta.LastModified.Add(-time.Hour).Format(http.TimeFormat)
	after := meta.LastModified.Add(time.Hour).Format(http.TimeFormat)
	for _, method := range []string{"GET", "HEAD"} {
		for _, tc := range []struct {
			name    string
			headers http.Header
			status  int
		}{
			{"match overrides unmodified", http.Header{"If-Match": {meta.ETag}, "If-Unmodified-Since": {before}}, 200},
			{"none-match overrides modified", http.Header{"If-None-Match": {"\"different\""}, "If-Modified-Since": {after}}, 200},
			{"unmodified precedes none-match", http.Header{"If-Unmodified-Since": {before}, "If-None-Match": {meta.ETag}}, 412},
			{"not-modified CORS", http.Header{"If-None-Match": {meta.ETag}}, 304},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				req := httptest.NewRequest(method, "/bucket/key", nil)
				req.Header = tc.headers.Clone()
				req.Header.Set("Origin", "http://localhost:3000")
				res := httptest.NewRecorder()
				srv.ServeHTTP(res, req)
				if res.Code != tc.status {
					t.Fatalf("status=%d expected=%d", res.Code, tc.status)
				}
				if tc.status == 304 && (res.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" || !strings.Contains(res.Header().Get("Vary"), "Origin")) {
					t.Fatalf("304 CORS=%v", res.Header())
				}
			})
		}
	}
}

func TestCopyRefusesUnsupportedDestinationConditions(t *testing.T) {
	for _, directive := range []string{"COPY", "REPLACE"} {
		t.Run(directive, func(t *testing.T) {
			srv, store := regressionServer(t)
			ctx := context.Background()
			for _, key := range []string{"source", "destination"} {
				if _, err := store.PutObject(ctx, "bucket", key, strings.NewReader(key), storage.PutOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest("PUT", "/bucket/destination", nil)
			req.Header.Set("X-Amz-Copy-Source", "/bucket/source")
			req.Header.Set("X-Amz-Metadata-Directive", directive)
			req.Header.Set("If-None-Match", "*")
			res := httptest.NewRecorder()
			srv.ServeHTTP(res, req)
			if res.Code != 501 {
				t.Fatalf("destination condition ignored: %d %s", res.Code, res.Body.String())
			}
			meta, err := store.HeadObject(ctx, "bucket", "destination")
			if err != nil {
				t.Fatal(err)
			}
			if meta.ETag != storage.ETagForBytes([]byte("destination")) {
				t.Fatal("conditional copy changed destination")
			}
		})
	}
}
