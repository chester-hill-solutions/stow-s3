package runthrough_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

func TestUpstreamTransportPolicy(t *testing.T) {
	for _, test := range []struct {
		endpoint          string
		insecure, allowed bool
	}{
		{"https://storage.example/bucket", false, true},
		{"http://127.0.0.1:9000", false, true},
		{"http://[::1]:9000", false, true},
		{"http://localhost:9000", false, false},
		{"http://storage.example", false, false},
		{"http://storage.example", true, true},
		{"https://user:secret@storage.example", true, false},
		{"https://storage.example/#fragment", true, false},
		{"file:///private/data", true, false},
		{"storage.example", true, false},
	} {
		t.Run(test.endpoint, func(t *testing.T) {
			_, err := runthrough.NewS3Client(runthrough.UpstreamConfig{Endpoint: test.endpoint, AllowInsecureHTTP: test.insecure})
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%v error=%v", test.allowed, err)
			}
		})
	}
}

func TestUpstreamRedirectNeverReachesAnotherOrigin(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var hits atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(http.StatusNoContent) }))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL+"/stolen", status) }))
			defer source.Close()
			client, err := runthrough.NewS3Client(runthrough.UpstreamConfig{Endpoint: source.URL, AccessKey: "test-access", SecretKey: "test-secret", SessionToken: "test-token"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.HeadObject(context.Background(), "bucket", "key"); err == nil {
				t.Fatal("cross-origin redirect succeeded")
			}
			if hits.Load() != 0 {
				t.Fatal("redirect target received a credential-bearing request")
			}
		})
	}
}

func TestUpstreamSameOriginRedirectPreservesRequest(t *testing.T) {
	var reached atomic.Bool
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/redirected" {
			http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
			return
		}
		if r.Method == http.MethodHead && r.Header.Get("Authorization") != "" && r.Header.Get("X-Amz-Security-Token") == "test-token" {
			reached.Store(true)
		}
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Content-Length", "0")
	}))
	defer source.Close()
	client, err := runthrough.NewS3Client(runthrough.UpstreamConfig{Endpoint: source.URL, AccessKey: "test-access", SecretKey: "test-secret", SessionToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.HeadObject(context.Background(), "bucket", "key"); err != nil {
		t.Fatal(err)
	}
	if !reached.Load() {
		t.Fatal("same-origin redirect did not preserve method and credential scope")
	}
}
