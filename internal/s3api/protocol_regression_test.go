package s3api

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func regressionServer(t *testing.T) (*Server, *storage.MemoryStore) {
	t.Helper()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(context.Background(), "bucket"); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{Store: store, Auth: DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	return srv, store
}

func TestMalformedRangeNeverPanics(t *testing.T) {
	srv, store := regressionServer(t)
	if _, err := store.PutObject(context.Background(), "bucket", "key", strings.NewReader("abc"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, hdr := range []string{"bytes=0", "bytes=", "bytes=-", "bytes=0-1,2-3", "bytes=+0-1", "bytes=0-+1", "bytes=--1"} {
		t.Run(hdr, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/bucket/key", nil)
			req.Header.Set("Range", hdr)
			res := httptest.NewRecorder()
			srv.ServeHTTP(res, req)
			if res.Code != http.StatusRequestedRangeNotSatisfiable {
				t.Fatalf("status %d: %s", res.Code, res.Body.String())
			}
		})
	}
	if _, _, err := parseRange("bytes=-1", 0); err == nil {
		t.Fatal("accepted range on empty object")
	}
}

func TestModifiedSinceReadReturnsNotModified(t *testing.T) {
	srv, store := regressionServer(t)
	meta, err := store.PutObject(context.Background(), "bucket", "key", strings.NewReader("abc"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "HEAD"} {
		req := httptest.NewRequest(method, "/bucket/key", nil)
		req.Header.Set("If-Modified-Since", meta.LastModified.Format(http.TimeFormat))
		res := httptest.NewRecorder()
		srv.ServeHTTP(res, req)
		if res.Code != 304 || res.Body.Len() != 0 {
			t.Errorf("%s: %d %s", method, res.Code, res.Body.String())
		}
	}
	headers := http.Header{"X-Amz-Copy-Source-If-Modified-Since": []string{meta.LastModified.Add(time.Second).Format(http.TimeFormat)}}
	if err := checkCopyPreconditions(headers, meta); err != storage.ErrPreconditionFailed {
		t.Fatalf("copy condition: %v", err)
	}
}

func TestHTTPListPartsPagination(t *testing.T) {
	srv, store := regressionServer(t)
	ctx := context.Background()
	upload, err := store.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 3, 7} {
		if _, err := store.UploadPart(ctx, upload.UploadID, n, strings.NewReader("part")); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query                            string
		status, marker, next, max, count int
		truncated                        bool
	}{
		{"max-parts=1&part-number-marker=1", 200, 1, 3, 1, 1, true},
		{"max-parts=1&part-number-marker=3", 200, 3, 0, 1, 1, false},
		{"max-parts=-1", 400, 0, 0, 0, 0, false}, {"part-number-marker=no", 400, 0, 0, 0, 0, false},
	} {
		req := httptest.NewRequest("GET", "/bucket/key?uploadId="+upload.UploadID+"&"+tc.query, nil)
		res := httptest.NewRecorder()
		srv.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Errorf("%s: status %d", tc.query, res.Code)
			continue
		}
		if tc.status != 200 {
			continue
		}
		var got listPartsResult
		if err := xml.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.PartNumberMarker != tc.marker || got.NextPartNumberMarker != tc.next || got.MaxParts != tc.max || len(got.Parts) != tc.count || got.IsTruncated != tc.truncated {
			t.Errorf("%s: %+v", tc.query, got)
		}
	}
}

func TestHTTPRejectsMalformedQueryBeforeAuthentication(t *testing.T) {
	srv, _ := regressionServer(t)
	called := false
	srv.auth = func(*http.Request) error { called = true; return nil }
	req := httptest.NewRequest("GET", "/bucket?list-type=2&bad=%ZZ", nil)
	res := httptest.NewRecorder()
	srv.ServeHTTP(res, req)
	if res.Code != 400 || called {
		t.Fatalf("malformed query: status %d, authenticated %v", res.Code, called)
	}
}
